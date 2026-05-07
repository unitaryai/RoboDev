package webhook

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
