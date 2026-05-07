package webhook

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	svix "github.com/svix/svix-webhooks/go"
)

const incidentCreatedV2Payload = `{
  "event_type": "public_incident.incident_created_v2",
  "public_incident.incident_created_v2": {
    "id": "01HZ0000000000000000000001",
    "reference": "INC-123",
    "name": "Database is down",
    "summary": "Customer reports the API is returning 500s",
    "permalink": "https://app.incident.io/incidents/123",
    "visibility": "public",
    "mode": "standard",
    "incident_status": {
      "id": "01HZ_status_live",
      "name": "Investigating",
      "category": "live",
      "rank": 10
    },
    "severity": {
      "id": "01HZ_sev_major",
      "name": "Major",
      "rank": 100
    },
    "incident_type": {
      "id": "01HZ_type_outage",
      "name": "Production outage"
    },
    "slack_team_id": "T0123",
    "slack_channel_id": "C0123",
    "slack_channel_name": "inc-database-down",
    "created_at": "2026-05-07T10:00:00Z",
    "updated_at": "2026-05-07T10:00:00Z"
  }
}`

const incidentStatusUpdatedV2Payload = `{
  "event_type": "public_incident.incident_status_updated_v2",
  "public_incident.incident_status_updated_v2": {
    "incident": {
      "id": "01HZ0000000000000000000002",
      "reference": "INC-456",
      "name": "Auth service degraded",
      "visibility": "public",
      "mode": "standard",
      "incident_status": {
        "id": "01HZ_status_closed",
        "name": "Closed",
        "category": "closed",
        "rank": 99
      },
      "slack_team_id": "T0123",
      "slack_channel_id": "C0456",
      "created_at": "2026-05-07T11:00:00Z",
      "updated_at": "2026-05-07T11:30:00Z"
    },
    "message": "Resolved after deploy rollback",
    "new_status": {
      "id": "01HZ_status_closed",
      "name": "Closed",
      "category": "closed",
      "rank": 99
    },
    "previous_status": {
      "id": "01HZ_status_live",
      "name": "Fixing",
      "category": "live",
      "rank": 11
    }
  }
}`

func TestParseIncidentEvent_CreatedV2(t *testing.T) {
	evt, err := ParseIncidentEvent([]byte(incidentCreatedV2Payload))
	require.NoError(t, err)

	assert.Equal(t, EventIncidentCreatedV2, evt.EventType)
	assert.Equal(t, "01HZ0000000000000000000001", evt.Incident.ID)
	assert.Equal(t, "INC-123", evt.Incident.Reference)
	assert.Equal(t, "Database is down", evt.Incident.Name)
	assert.Equal(t, "Customer reports the API is returning 500s", evt.Incident.Summary)
	assert.Equal(t, "https://app.incident.io/incidents/123", evt.Incident.Permalink)
	assert.Equal(t, "public", evt.Incident.Visibility)
	assert.Equal(t, "standard", evt.Incident.Mode)

	assert.Equal(t, "01HZ_status_live", evt.Incident.IncidentStatus.ID)
	assert.Equal(t, "Investigating", evt.Incident.IncidentStatus.Name)
	assert.Equal(t, "live", evt.Incident.IncidentStatus.Category)
	assert.Equal(t, 10, evt.Incident.IncidentStatus.Rank)

	require.NotNil(t, evt.Incident.Severity)
	assert.Equal(t, "Major", evt.Incident.Severity.Name)
	assert.Equal(t, 100, evt.Incident.Severity.Rank)

	require.NotNil(t, evt.Incident.IncidentType)
	assert.Equal(t, "Production outage", evt.Incident.IncidentType.Name)

	assert.Equal(t, "T0123", evt.Incident.SlackTeamID)
	assert.Equal(t, "C0123", evt.Incident.SlackChannelID)
	assert.Equal(t, "inc-database-down", evt.Incident.SlackChannelName)
	assert.Equal(t, time.Date(2026, 5, 7, 10, 0, 0, 0, time.UTC), evt.Incident.CreatedAt)

	// Status-update-only fields must be zero/nil for a created event.
	assert.Empty(t, evt.Message)
	assert.Nil(t, evt.NewStatus)
	assert.Nil(t, evt.PreviousStatus)

	// Raw should round-trip the original body verbatim.
	assert.JSONEq(t, incidentCreatedV2Payload, string(evt.Raw))
}

func TestParseIncidentEvent_StatusUpdatedV2(t *testing.T) {
	evt, err := ParseIncidentEvent([]byte(incidentStatusUpdatedV2Payload))
	require.NoError(t, err)

	assert.Equal(t, EventIncidentStatusUpdatedV2, evt.EventType)
	assert.Equal(t, "01HZ0000000000000000000002", evt.Incident.ID)
	assert.Equal(t, "INC-456", evt.Incident.Reference)
	assert.Equal(t, "Auth service degraded", evt.Incident.Name)
	assert.Equal(t, "closed", evt.Incident.IncidentStatus.Category)

	// Severity and IncidentType are optional and absent in this payload.
	assert.Nil(t, evt.Incident.Severity)
	assert.Nil(t, evt.Incident.IncidentType)

	assert.Equal(t, "Resolved after deploy rollback", evt.Message)

	require.NotNil(t, evt.NewStatus)
	assert.Equal(t, "Closed", evt.NewStatus.Name)
	assert.Equal(t, "closed", evt.NewStatus.Category)

	require.NotNil(t, evt.PreviousStatus)
	assert.Equal(t, "Fixing", evt.PreviousStatus.Name)
	assert.Equal(t, "live", evt.PreviousStatus.Category)
}

func TestParseIncidentEvent_Errors(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantInErr string
	}{
		{
			name:      "malformed JSON",
			body:      `{not json`,
			wantInErr: "decoding webhook envelope",
		},
		{
			name:      "missing event_type",
			body:      `{"public_incident.incident_created_v2": {"id": "x"}}`,
			wantInErr: "missing required \"event_type\" field",
		},
		{
			name:      "empty event_type",
			body:      `{"event_type": "", "public_incident.incident_created_v2": {"id": "x"}}`,
			wantInErr: "empty event_type",
		},
		{
			name:      "missing wrapper key",
			body:      `{"event_type": "public_incident.incident_created_v2"}`,
			wantInErr: "missing wrapper key \"public_incident.incident_created_v2\"",
		},
		{
			name:      "unsupported event_type",
			body:      `{"event_type": "public_incident.incident_deleted_v2", "public_incident.incident_deleted_v2": {}}`,
			wantInErr: "unsupported event_type \"public_incident.incident_deleted_v2\"",
		},
		{
			name:      "missing incident.id",
			body:      `{"event_type": "public_incident.incident_created_v2", "public_incident.incident_created_v2": {"reference": "INC-1"}}`,
			wantInErr: "empty incident.id",
		},
		{
			name:      "malformed wrapped body",
			body:      `{"event_type": "public_incident.incident_created_v2", "public_incident.incident_created_v2": "not an object"}`,
			wantInErr: "decoding public_incident.incident_created_v2 body",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseIncidentEvent([]byte(tc.body))
			require.Error(t, err)
			assert.True(t, strings.Contains(err.Error(), tc.wantInErr),
				"expected error to contain %q, got %q", tc.wantInErr, err.Error())
		})
	}
}

// Ensure the IncidentHandler interface compiles against a no-op implementation.
// This guards against accidental signature drift in subsequent commits that
// add the server handler and reconciler method.
var _ IncidentHandler = (*noopIncidentHandler)(nil)

type noopIncidentHandler struct{}

func (noopIncidentHandler) HandleIncidentEvent(_ context.Context, _ IncidentEvent) error {
	return nil
}

// mockIncidentHandler captures HandleIncidentEvent invocations for assertion.
// err, when non-nil, is returned to simulate downstream failure paths.
type mockIncidentHandler struct {
	calls []IncidentEvent
	err   error
}

func (m *mockIncidentHandler) HandleIncidentEvent(_ context.Context, evt IncidentEvent) error {
	m.calls = append(m.calls, evt)
	return m.err
}

// newSvixSecret returns a fresh whsec_-prefixed Svix signing key.
// Svix verifies HMAC-SHA256 with 32-byte keys; using random bytes makes
// the size expectation explicit and avoids cross-test interference.
func newSvixSecret(t *testing.T) string {
	t.Helper()
	keyBytes := make([]byte, 32)
	_, err := rand.Read(keyBytes)
	require.NoError(t, err)
	return "whsec_" + base64.StdEncoding.EncodeToString(keyBytes)
}

// signSvix attaches valid svix-* headers to a request using the given
// secret and timestamp. msgID is the message identifier echoed in
// svix-id; any non-empty value works for verification.
func signSvix(t *testing.T, req *http.Request, secret string, body []byte, ts time.Time, msgID string) {
	t.Helper()
	wh, err := svix.NewWebhook(secret)
	require.NoError(t, err)
	sig, err := wh.Sign(msgID, ts, body)
	require.NoError(t, err)
	req.Header.Set("svix-id", msgID)
	req.Header.Set("svix-timestamp", strconv.FormatInt(ts.Unix(), 10))
	req.Header.Set("svix-signature", sig)
}

func TestHandleIncidentIO(t *testing.T) {
	secret := newSvixSecret(t)
	wrongSecret := newSvixSecret(t)

	tests := []struct {
		name       string
		body       string
		signSecret string // empty means no signature headers attached
		signAt     time.Time
		wantStatus int
		wantCalls  int
		// assertEvent runs against the captured event when wantCalls == 1.
		assertEvent func(t *testing.T, evt IncidentEvent)
	}{
		{
			name:       "valid created_v2 payload",
			body:       incidentCreatedV2Payload,
			signSecret: secret,
			signAt:     time.Now(),
			wantStatus: http.StatusOK,
			wantCalls:  1,
			assertEvent: func(t *testing.T, evt IncidentEvent) {
				assert.Equal(t, EventIncidentCreatedV2, evt.EventType)
				assert.Equal(t, "01HZ0000000000000000000001", evt.Incident.ID)
				assert.Equal(t, "INC-123", evt.Incident.Reference)
				assert.Nil(t, evt.NewStatus)
			},
		},
		{
			name:       "valid status_updated_v2 payload",
			body:       incidentStatusUpdatedV2Payload,
			signSecret: secret,
			signAt:     time.Now(),
			wantStatus: http.StatusOK,
			wantCalls:  1,
			assertEvent: func(t *testing.T, evt IncidentEvent) {
				assert.Equal(t, EventIncidentStatusUpdatedV2, evt.EventType)
				require.NotNil(t, evt.NewStatus)
				assert.Equal(t, "closed", evt.NewStatus.Category)
				require.NotNil(t, evt.PreviousStatus)
				assert.Equal(t, "live", evt.PreviousStatus.Category)
			},
		},
		{
			name:       "wrong secret rejected",
			body:       incidentCreatedV2Payload,
			signSecret: wrongSecret,
			signAt:     time.Now(),
			wantStatus: http.StatusUnauthorized,
			wantCalls:  0,
		},
		{
			name:       "stale timestamp rejected",
			body:       incidentCreatedV2Payload,
			signSecret: secret,
			signAt:     time.Now().Add(-10 * time.Minute),
			wantStatus: http.StatusUnauthorized,
			wantCalls:  0,
		},
		{
			name:       "missing signature headers rejected",
			body:       incidentCreatedV2Payload,
			signSecret: "",
			wantStatus: http.StatusUnauthorized,
			wantCalls:  0,
		},
		{
			name:       "malformed JSON returns 400",
			body:       `{not json`,
			signSecret: secret,
			signAt:     time.Now(),
			wantStatus: http.StatusBadRequest,
			wantCalls:  0,
		},
		{
			name:       "unsupported event_type returns 400",
			body:       `{"event_type": "public_incident.incident_deleted_v2", "public_incident.incident_deleted_v2": {"id": "x"}}`,
			signSecret: secret,
			signAt:     time.Now(),
			wantStatus: http.StatusBadRequest,
			wantCalls:  0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &IncidentIOConfig{AuthMode: "svix", Secret: secret}
			handler := &mockIncidentHandler{}
			srv, err := NewServer(testLogger(), &mockEventHandler{},
				WithIncidentConfig(cfg),
				WithIncidentHandler(handler),
			)
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodPost, "/webhooks/incident-io",
				bytes.NewReader([]byte(tc.body)))
			req.Header.Set("Content-Type", "application/json")

			if tc.signSecret != "" {
				signSvix(t, req, tc.signSecret, []byte(tc.body), tc.signAt, "msg_test_001")
			}

			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)

			assert.Equal(t, tc.wantStatus, rec.Code)
			assert.Len(t, handler.calls, tc.wantCalls)

			if tc.wantCalls == 1 && tc.assertEvent != nil {
				tc.assertEvent(t, handler.calls[0])
			}
		})
	}
}

func TestHandleIncidentIO_NotConfigured(t *testing.T) {
	// Server built without WithIncidentConfig: the route exists but the
	// endpoint must respond 500 rather than panic on a nil config.
	srv, err := NewServer(testLogger(), &mockEventHandler{})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/incident-io",
		bytes.NewReader([]byte(incidentCreatedV2Payload)))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestHandleIncidentIO_NoHandlerConfigured(t *testing.T) {
	// Server has incidentConfig but no IncidentHandler. Verification
	// succeeds but dispatch has nowhere to go — 500 rather than dropping
	// the event silently.
	secret := newSvixSecret(t)
	cfg := &IncidentIOConfig{AuthMode: "svix", Secret: secret}
	srv, err := NewServer(testLogger(), &mockEventHandler{}, WithIncidentConfig(cfg))
	require.NoError(t, err)

	body := []byte(incidentCreatedV2Payload)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/incident-io",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	signSvix(t, req, secret, body, time.Now(), "msg_test_002")

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestHandleIncidentIO_HandlerError(t *testing.T) {
	// When the IncidentHandler returns an error, the server must respond
	// 5xx so the sender (incident.io / Svix) retries the delivery.
	secret := newSvixSecret(t)
	cfg := &IncidentIOConfig{AuthMode: "svix", Secret: secret}
	handler := &mockIncidentHandler{err: fmt.Errorf("downstream boom")}
	srv, err := NewServer(testLogger(), &mockEventHandler{},
		WithIncidentConfig(cfg),
		WithIncidentHandler(handler),
	)
	require.NoError(t, err)

	body := []byte(incidentCreatedV2Payload)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/incident-io",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	signSvix(t, req, secret, body, time.Now(), "msg_test_003")

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Len(t, handler.calls, 1, "handler was invoked even though it returned an error")
}

func TestIncidentIOConfig_Prepare(t *testing.T) {
	tests := []struct {
		name      string
		cfg       *IncidentIOConfig
		wantInErr string
	}{
		{
			name: "valid svix config",
			cfg:  &IncidentIOConfig{AuthMode: "svix", Secret: newSvixSecret(t)},
		},
		{
			name:      "missing secret",
			cfg:       &IncidentIOConfig{AuthMode: "svix", Secret: ""},
			wantInErr: "secret is required",
		},
		{
			name:      "missing auth_mode",
			cfg:       &IncidentIOConfig{AuthMode: "", Secret: newSvixSecret(t)},
			wantInErr: "auth_mode is required",
		},
		{
			name:      "unsupported auth_mode",
			cfg:       &IncidentIOConfig{AuthMode: "hmac", Secret: newSvixSecret(t)},
			wantInErr: "unsupported auth_mode \"hmac\"",
		},
		{
			name:      "malformed svix secret",
			cfg:       &IncidentIOConfig{AuthMode: "svix", Secret: "not-a-real-key"},
			wantInErr: "invalid svix secret",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Prepare()
			if tc.wantInErr == "" {
				assert.NoError(t, err)
				assert.NotNil(t, tc.cfg.svixWebhook, "Prepare must populate the verifier on success")
				return
			}
			require.Error(t, err)
			assert.True(t, strings.Contains(err.Error(), tc.wantInErr),
				"expected error to contain %q, got %q", tc.wantInErr, err.Error())
		})
	}
}
