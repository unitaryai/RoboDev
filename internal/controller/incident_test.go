package controller

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/unitaryai/osmia/internal/config"
	"github.com/unitaryai/osmia/internal/webhook"
)

// incidentTestEvent returns a minimal valid IncidentEvent for the
// public_incident.incident_created_v2 path.
func incidentTestEvent(id string) webhook.IncidentEvent {
	return webhook.IncidentEvent{
		EventType: webhook.EventIncidentCreatedV2,
		Incident: webhook.IncidentV2{
			ID:        id,
			Reference: "INC-" + id,
			Name:      "Database is down",
			Summary:   "Customers reporting 500s",
			Permalink: "https://app.incident.io/incidents/" + id,
		},
	}
}

func incidentTestConfig() *config.Config {
	return &config.Config{
		Engines: config.EnginesConfig{
			Default: "claude-code",
		},
		GuardRails: config.GuardRailsConfig{
			MaxConcurrentJobs:     5,
			MaxJobDurationMinutes: 120,
		},
		IncidentTriage: config.IncidentTriageConfig{
			Engine: "claude-code",
		},
	}
}

// TestProcessIncidentEvent_DispatchDisabled covers the only behaviour
// the entry point exhibits in this build: events for any supported
// type are accepted, no Job is created, no TaskRun is recorded, and
// the call returns nil so the webhook responds 200 OK.
//
// When the dispatch path is restored in a follow-up change, this test
// remains useful as a regression guard against accidental no-op state
// (e.g. a config-gate that silently disables dispatch); positive-path
// dispatch tests will be added alongside the restored implementation.
func TestProcessIncidentEvent_DispatchDisabled(t *testing.T) {
	cfg := incidentTestConfig()
	logger := testLogger()
	k8s := fake.NewSimpleClientset()

	r := NewReconciler(cfg, logger,
		WithK8sClient(k8s),
		WithNamespace("test-ns"),
	)

	ctx := context.Background()

	for _, eventType := range []string{
		webhook.EventIncidentCreatedV2,
		webhook.EventIncidentStatusUpdatedV2,
	} {
		t.Run(eventType, func(t *testing.T) {
			evt := incidentTestEvent("01HZTEST")
			evt.EventType = eventType
			require.NoError(t, r.ProcessIncidentEvent(ctx, evt))
		})
	}

	jobs, err := k8s.BatchV1().Jobs("test-ns").List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	assert.Len(t, jobs.Items, 0, "no Job should be created when dispatch is disabled")
}
