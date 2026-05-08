package webhook

// This file handles incident.io webhook deliveries on a path that is
// deliberately separate from the SCM ticketing flow in `handleGeneric`.
// The parser, types, and HTTP handler are intentionally incident.io-
// specific rather than abstracted: a future refactor will lift both
// flows behind a common interface once a second non-ticketing consumer
// exists to inform the abstraction's shape.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	svix "github.com/svix/svix-webhooks/go"
)

// Supported incident.io webhook event types. incident.io wraps each event
// payload in an object whose key matches `event_type` (an "event-namespaced
// wrapper" in their docs). Adding a new event type means adding a constant
// and a case in ParseIncidentEvent.
const (
	EventIncidentCreatedV2       = "public_incident.incident_created_v2"
	EventIncidentStatusUpdatedV2 = "public_incident.incident_status_updated_v2"
)

// IncidentHandler processes parsed incident.io webhook events. It is the
// dispatch target for /webhooks/incident-io and intentionally has no
// awareness of HTTP — the server handler validates and parses the request
// before invoking this interface, mirroring the ApprovalHandler pattern.
type IncidentHandler interface {
	HandleIncidentEvent(ctx context.Context, evt IncidentEvent) error
}

// IncidentIOConfig holds the runtime configuration for the incident.io
// webhook handler, mirroring the GenericConfig pattern. Currently only
// Svix-style signing is supported (which is what incident.io emits); the
// AuthMode field is preserved for future flexibility.
//
// Callers must invoke Prepare() at startup after populating the fields so
// the Svix verifier is constructed once rather than per-request. Skipping
// Prepare() leaves Svix mode unusable — every request will be rejected
// with a 500 because the verifier is nil.
type IncidentIOConfig struct {
	// AuthMode is the authentication method. Only "svix" is currently
	// supported.
	AuthMode string

	// Secret is the Svix signing key. A "whsec_" prefix is recognised and
	// the remainder is base64-decoded by the Svix library before use.
	Secret string

	// svixWebhook is the pre-constructed Svix verifier, populated by
	// Prepare().
	svixWebhook *svix.Webhook
}

// Prepare validates the configuration and pre-constructs the Svix
// verifier. It is idempotent; repeat calls re-run validation and rebuild
// the verifier. Any error path clears the existing verifier so that a
// previously-successful Prepare followed by a failed re-prepare leaves
// the config in a fail-closed state rather than silently accepting the
// old secret.
func (i *IncidentIOConfig) Prepare() error {
	i.svixWebhook = nil
	if i.Secret == "" {
		return fmt.Errorf("secret is required")
	}
	switch i.AuthMode {
	case "svix":
		wh, err := svix.NewWebhook(i.Secret)
		if err != nil {
			return fmt.Errorf("invalid svix secret: %w", err)
		}
		i.svixWebhook = wh
	case "":
		return fmt.Errorf("auth_mode is required")
	default:
		return fmt.Errorf("unsupported auth_mode %q (only \"svix\" is supported)", i.AuthMode)
	}
	return nil
}

// IncidentEvent is a parsed incident.io webhook delivery, suitable for
// passing into the controller's reconciliation pipeline. The fields that
// are present depend on EventType:
//
//   - EventIncidentCreatedV2: only Incident is populated.
//   - EventIncidentStatusUpdatedV2: Incident, Message, NewStatus, and
//     PreviousStatus are populated.
//
// Raw retains the original request body so consumers (e.g. the agent
// prompt builder) can surface payload fields not modelled in the typed
// structs without re-fetching from incident.io.
type IncidentEvent struct {
	EventType      string
	Incident       IncidentV2
	Message        string
	NewStatus      *IncidentStatusV2
	PreviousStatus *IncidentStatusV2
	Raw            json.RawMessage
}

// IncidentV2 captures the fields of incident.io's IncidentV2 schema that
// the triage classifier reasons over. Long-form description strings on
// nested objects, rich relations (creator, role assignments, custom field
// entries, related incidents), and operational metrics are intentionally
// omitted; consumers needing them can read from IncidentEvent.Raw.
type IncidentV2 struct {
	ID               string           `json:"id"`
	Reference        string           `json:"reference"`
	Name             string           `json:"name"`
	Summary          string           `json:"summary,omitempty"`
	Permalink        string           `json:"permalink,omitempty"`
	Visibility       string           `json:"visibility"`
	Mode             string           `json:"mode"`
	IncidentStatus   IncidentStatusV2 `json:"incident_status"`
	Severity         *SeverityV2      `json:"severity,omitempty"`
	IncidentType     *IncidentTypeV2  `json:"incident_type,omitempty"`
	SlackTeamID      string           `json:"slack_team_id"`
	SlackChannelID   string           `json:"slack_channel_id"`
	SlackChannelName string           `json:"slack_channel_name,omitempty"`
	CreatedAt        time.Time        `json:"created_at"`
	UpdatedAt        time.Time        `json:"updated_at"`
}

// IncidentStatusV2 captures the lifecycle status of an incident. The
// Category field is one of incident.io's documented enum values:
// "triage", "declined", "merged", "canceled", "live", "learning", "closed".
type IncidentStatusV2 struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"`
	Rank     int    `json:"rank"`
}

// SeverityV2 represents an incident severity tier (e.g. SEV1, SEV2).
type SeverityV2 struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Rank int    `json:"rank"`
}

// IncidentTypeV2 represents an incident type (e.g. "Production outage").
type IncidentTypeV2 struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// incidentStatusUpdatedBody mirrors the body of the
// public_incident.incident_status_updated_v2 event, which wraps the
// incident object alongside the previous and new status references.
type incidentStatusUpdatedBody struct {
	Incident       IncidentV2        `json:"incident"`
	Message        string            `json:"message"`
	NewStatus      *IncidentStatusV2 `json:"new_status"`
	PreviousStatus *IncidentStatusV2 `json:"previous_status"`
}

// handleIncidentIO processes incoming incident.io webhook deliveries.
// Verification is delegated to the shared Svix helper on Server; parsing
// is delegated to ParseIncidentEvent; dispatch is delegated to the
// configured IncidentHandler. The handler intentionally bypasses the
// ticketing pipeline (handleGeneric → ProcessTicket) — see the file
// header for the rationale.
func (s *Server) handleIncidentIO(w http.ResponseWriter, r *http.Request) {
	if s.incidentConfig == nil {
		s.logger.Error("incident.io webhook not configured")
		http.Error(w, "incident.io webhook not configured", http.StatusInternalServerError)
		return
	}

	body, ok := s.readRequestBody(w, r)
	if !ok {
		return
	}

	if !s.verifySvixSignature(w, r, body, s.incidentConfig.svixWebhook, "incident-io") {
		return
	}

	evt, err := ParseIncidentEvent(body)
	if err != nil {
		s.logger.Warn("malformed incident.io payload", slog.String("error", err.Error()))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if s.incidentHandler == nil {
		s.logger.Error("incident.io webhook handler not configured",
			slog.String("event_type", evt.EventType),
			slog.String("incident_id", evt.Incident.ID))
		http.Error(w, "no handler configured", http.StatusInternalServerError)
		return
	}

	if err := s.incidentHandler.HandleIncidentEvent(r.Context(), evt); err != nil {
		s.logger.Error("incident.io handler returned error",
			slog.String("event_type", evt.EventType),
			slog.String("incident_id", evt.Incident.ID),
			slog.String("error", err.Error()))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.logger.Info("processed incident.io webhook",
		slog.String("event_type", evt.EventType),
		slog.String("incident_id", evt.Incident.ID),
		slog.String("incident_reference", evt.Incident.Reference))
	w.WriteHeader(http.StatusOK)
}

// ParseIncidentEvent decodes an incident.io webhook delivery body. The
// caller is responsible for verifying the request signature before
// invoking this function — the parser trusts the bytes it is given.
//
// Unsupported event types are rejected explicitly rather than silently
// ignored so that future incident.io webhook subscriptions surface as
// 4xx responses (and thus visible failures in their delivery dashboard)
// until handlers exist for them.
func ParseIncidentEvent(body []byte) (IncidentEvent, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return IncidentEvent{}, fmt.Errorf("decoding webhook envelope: %w", err)
	}

	rawEventType, ok := envelope["event_type"]
	if !ok {
		return IncidentEvent{}, fmt.Errorf("webhook payload missing required \"event_type\" field")
	}
	var eventType string
	if err := json.Unmarshal(rawEventType, &eventType); err != nil {
		return IncidentEvent{}, fmt.Errorf("decoding event_type: %w", err)
	}
	if eventType == "" {
		return IncidentEvent{}, fmt.Errorf("webhook payload has empty event_type")
	}

	wrapped, ok := envelope[eventType]
	if !ok {
		return IncidentEvent{}, fmt.Errorf("webhook payload missing wrapper key %q matching event_type", eventType)
	}

	evt := IncidentEvent{
		EventType: eventType,
		Raw:       append(json.RawMessage(nil), body...),
	}

	switch eventType {
	case EventIncidentCreatedV2:
		if err := json.Unmarshal(wrapped, &evt.Incident); err != nil {
			return IncidentEvent{}, fmt.Errorf("decoding %s body: %w", eventType, err)
		}
	case EventIncidentStatusUpdatedV2:
		var w incidentStatusUpdatedBody
		if err := json.Unmarshal(wrapped, &w); err != nil {
			return IncidentEvent{}, fmt.Errorf("decoding %s body: %w", eventType, err)
		}
		// new_status and previous_status are the whole point of this event
		// type; reject signed-but-malformed deliveries that omit either
		// rather than dispatching a triage run without transition context.
		// An empty JSON object ({}) deserialises to a non-nil pointer with
		// zero-value fields, so the nil-pointer check alone is not enough —
		// also require a non-empty status ID, mirroring the Incident.ID
		// guard for incident_created_v2.
		if w.NewStatus == nil {
			return IncidentEvent{}, fmt.Errorf("%s payload missing required new_status field", eventType)
		}
		if w.NewStatus.ID == "" {
			return IncidentEvent{}, fmt.Errorf("%s payload has empty new_status.id", eventType)
		}
		if w.PreviousStatus == nil {
			return IncidentEvent{}, fmt.Errorf("%s payload missing required previous_status field", eventType)
		}
		if w.PreviousStatus.ID == "" {
			return IncidentEvent{}, fmt.Errorf("%s payload has empty previous_status.id", eventType)
		}
		evt.Incident = w.Incident
		evt.Message = w.Message
		evt.NewStatus = w.NewStatus
		evt.PreviousStatus = w.PreviousStatus
	default:
		return IncidentEvent{}, fmt.Errorf("unsupported event_type %q", eventType)
	}

	if evt.Incident.ID == "" {
		return IncidentEvent{}, fmt.Errorf("webhook payload has empty incident.id")
	}

	return evt, nil
}
