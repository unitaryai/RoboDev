package controller

// ProcessIncidentEvent is a reconciler entry point that runs in parallel
// to ProcessTicket. It handles the incident.io webhook flow, where the
// agent is launched without a repository or merge-request lifecycle.
// The two flows now share their task-launch tail (see launchTaskRun in
// launch.go: prepareSession through the stream-reader start), but each
// keeps its own front half — gates, repo-URL resolution, engine
// selection, memory queries, and per-flow EngineConfig overrides — since
// those genuinely differ between a ticketing-backed run and an
// incident-triage run. A future refactor may lift both behind a common
// use-case interface (see docs/designs/use-case-abstraction.md); until
// then, the duplication in the front half is bounded and intentional.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/unitaryai/osmia/internal/webhook"
	"github.com/unitaryai/osmia/pkg/engine"
)

// defaultIncidentEngine is the engine used when IncidentTriage.Engine is
// unset. claude-code is the typical default; operators can override via
// config to dispatch to any registered engine.
const defaultIncidentEngine = "claude-code"

// eventTypeSuffix maps an incident.io event type to a short
// DNS-1123-safe code used as part of the TaskRun ID (and hence the K8s
// Job name and label values). Short codes are required because both K8s
// names and label values are bounded at 63 characters; combined with a
// 26-char incident ULID and a millisecond timestamp, the verbose form
// of the event type would exceed that.
//
// Unknown event types should never reach this function — the webhook
// parser rejects them before dispatch — but the default returns a
// length-bounded marker rather than panicking.
func eventTypeSuffix(eventType string) string {
	switch eventType {
	case webhook.EventIncidentCreatedV2:
		return "created"
	case webhook.EventIncidentStatusUpdatedV2:
		return "updated"
	default:
		return "evt"
	}
}

// ProcessIncidentEvent launches an agent run in response to an
// incident.io webhook event, bypassing the SCM ticketing pipeline. It is
// the entry point that webhook.IncidentHandler implementations should
// call (typically through an adapter in cmd/osmia/main.go).
//
// Idempotency: keyed on incident_id:event_type so that "incident_created_v2"
// and "incident_status_updated_v2" for the same incident produce distinct
// task runs. Same key with the existing run not yet terminal returns nil
// (no-op); same key with a terminated run falls through to launch a fresh
// run, mirroring ProcessTicket's behaviour.
//
// What this skips compared to ProcessTicket: the engine selector chain
// (engine name comes directly from config.IncidentTriage.Engine), cost
// estimation (no historical data for incidents), the tournament
// coordinator (single-engine flow), the pre-start approval gate, the
// episodic memory query, runNotifyStart (the agent posts to Slack itself
// via MCP), and ticketing.MarkInProgress (no ticketing backend knows
// about incident UUIDs).
//
// Known limitation: when this TaskRun completes, handleJobComplete will
// call r.ticketing.MarkComplete with the incident ID as the TicketID,
// which the configured ticketing backend will not recognise. The
// resulting "not found" error is logged but non-fatal. The clean fix
// requires the use-case-aware completion-handler dispatch that the
// upcoming abstraction will provide.
func (r *Reconciler) ProcessIncidentEvent(ctx context.Context, evt webhook.IncidentEvent) error {
	idempotencyKey := evt.Incident.ID + ":" + evt.EventType

	r.mu.RLock()
	if existing, ok := r.taskRuns[idempotencyKey]; ok {
		r.mu.RUnlock()
		if !existing.IsTerminal() {
			r.logger.InfoContext(ctx, "incident task run already exists, skipping",
				"incident_id", evt.Incident.ID,
				"event_type", evt.EventType,
				"state", existing.State,
			)
			return nil
		}
	} else {
		r.mu.RUnlock()
	}

	engineName := r.config.IncidentTriage.Engine
	if engineName == "" {
		engineName = defaultIncidentEngine
	}
	eng, ok := r.engines[engineName]
	if !ok {
		return fmt.Errorf("incident triage engine %q not registered", engineName)
	}

	task := engine.Task{
		ID:          evt.Incident.ID,
		TicketID:    evt.Incident.ID,
		Title:       evt.Incident.Name,
		Description: enrichedDescription(evt.Incident),
		TicketURL:   evt.Incident.Permalink,
		Labels:      incidentLabels(evt),
	}

	// TaskRun ID must be DNS-1123 (lowercase alphanumeric + hyphens) since
	// it becomes part of the K8s Job name. Lowercasing the incident ID
	// keeps real ULID-shaped values valid; including a sanitised event-type
	// suffix prevents collisions when the same incident produces both a
	// created and a status_updated event in the same millisecond.
	trID := fmt.Sprintf("tr-incident-%s-%s-%d",
		strings.ToLower(evt.Incident.ID),
		eventTypeSuffix(evt.EventType),
		time.Now().UnixMilli(),
	)
	tr := r.newLaunchTaskRun(ctx, trID, idempotencyKey, task.TicketID, engineName)

	task.TaskRunID = tr.ID

	engineCfg := r.baseEngineConfig(ctx, engineName)
	if prompt := r.config.IncidentTriage.AppendSystemPrompt; prompt != "" {
		engineCfg.AppendSystemPrompt = prompt
	}
	// Per-flow Slack config. The ticketing flow reads its Slack channel
	// from the first entry in Notifications.Channels via slackEnv /
	// slackSecretKeyRefs; the incident-triage flow reads its channel
	// from the IncidentTriage block. Both flows are first-class — a
	// single Osmia deployment can run them side-by-side with separate
	// channels and bot tokens. When IncidentTriage.SlackChannelID /
	// SlackTokenSecret are empty, the incident flow falls back to the
	// ticketing channel for backward compatibility with single-channel
	// deployments. The use-case abstraction will eventually move all
	// per-flow Slack config behind a common interface.
	if ch := r.config.IncidentTriage.SlackChannelID; ch != "" {
		if engineCfg.Env == nil {
			engineCfg.Env = make(map[string]string)
		}
		engineCfg.Env["SLACK_CHANNEL_ID"] = ch
	}
	if tokenSecret := r.config.IncidentTriage.SlackTokenSecret; tokenSecret != "" {
		if engineCfg.SecretKeyRefs == nil {
			engineCfg.SecretKeyRefs = make(map[string]engine.SecretKeyRef)
		}
		engineCfg.SecretKeyRefs["SLACK_BOT_TOKEN"] = engine.SecretKeyRef{
			SecretName: tokenSecret,
			Key:        r.resolveSlackTokenKey(ctx, tokenSecret),
		}
	}
	// Per-flow incident.io MCP credentials. The agent's MCP client reads
	// INCIDENT_IO_API_KEY from the pod env to authenticate to
	// mcp.incident.io. Only the incident-triage flow needs this — ticketing
	// pods skip it. setup-claude.sh registers the incident.io MCP server in
	// the workspace mcp.json when this env var is present at job startup.
	if apiKeySecret := r.config.IncidentTriage.IncidentIOAPIKeySecret; apiKeySecret != "" {
		if engineCfg.SecretKeyRefs == nil {
			engineCfg.SecretKeyRefs = make(map[string]engine.SecretKeyRef)
		}
		engineCfg.SecretKeyRefs["INCIDENT_IO_API_KEY"] = engine.SecretKeyRef{
			SecretName: apiKeySecret,
			Key:        "INCIDENT_IO_API_KEY",
		}
	}

	_, err := r.launchTaskRun(ctx, launchSpec{
		TaskRun:        tr,
		IdempotencyKey: idempotencyKey,
		EngineName:     engineName,
		Engine:         eng,
		EngineChain:    []string{engineName},
		Task:           task,
		EngineConfig:   engineCfg,
		LogMessage:     "incident triage job created",
		LogFields:      []any{"incident_id", evt.Incident.ID, "event_type", evt.EventType},
	})
	return err
}

// incidentLabels assembles the `osmia:*:*` labels the agent's user prompt
// will carry for this dispatch. The values are restricted to fields whose
// shape is documented and bounded — enum-like categoricals and the
// short, alphanumeric+hyphen reference — so they pass through unchanged
// without sanitisation. Operator-defined names (severity.Name,
// incident_type.Name) and operator-curated structures (custom fields)
// are intentionally left for the agent to fetch via the incident.io
// API/MCP once that integration is wired.
func incidentLabels(evt webhook.IncidentEvent) []string {
	labels := []string{
		"osmia:source:incident-io",
		"osmia:event:" + evt.EventType,
	}
	if cat := evt.Incident.IncidentStatus.Category; cat != "" {
		labels = append(labels, "osmia:incident-status:"+cat)
	}
	if mode := evt.Incident.Mode; mode != "" {
		labels = append(labels, "osmia:mode:"+mode)
	}
	if ref := evt.Incident.Reference; ref != "" {
		labels = append(labels, "osmia:incident-reference:"+ref)
	}
	return labels
}

// enrichedDescription returns the incident summary, optionally followed
// by an "Underlying alert" prose block when the incident was alert-
// driven. Other creator types (user, webhook, manual) leave the summary
// untouched. Kept narrow on purpose: the goal is just to give the agent
// the underlying alert's title/ID directly rather than making it infer
// them from the summary text; broader incident context is fetched via
// the incident.io API/MCP when needed.
func enrichedDescription(inc webhook.IncidentV2) string {
	if inc.Creator == nil || inc.Creator.Alert == nil {
		return inc.Summary
	}
	alert := inc.Creator.Alert
	if alert.Title == "" && alert.ID == "" {
		return inc.Summary
	}
	var block strings.Builder
	block.WriteString(inc.Summary)
	if inc.Summary != "" {
		block.WriteString("\n\n")
	}
	block.WriteString("## Underlying alert\n\n")
	if alert.Title != "" {
		block.WriteString(alert.Title)
	}
	if alert.ID != "" {
		if alert.Title != "" {
			block.WriteString(" ")
		}
		block.WriteString("(alert id: " + alert.ID + ")")
	}
	return block.String()
}
