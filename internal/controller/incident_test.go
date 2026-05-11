package controller

import (
	"context"
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/unitaryai/osmia/internal/config"
	"github.com/unitaryai/osmia/internal/taskrun"
	"github.com/unitaryai/osmia/internal/webhook"
	"github.com/unitaryai/osmia/pkg/engine"
)

// recordingEngine captures BuildExecutionSpec arguments so tests can
// assert on the per-call EngineConfig override (AppendSystemPrompt).
type recordingEngine struct {
	name        string
	specErr     error
	lastTask    engine.Task
	lastConfig  engine.EngineConfig
	buildCalled int
}

func (m *recordingEngine) BuildExecutionSpec(task engine.Task, cfg engine.EngineConfig) (*engine.ExecutionSpec, error) {
	m.buildCalled++
	m.lastTask = task
	m.lastConfig = cfg
	if m.specErr != nil {
		return nil, m.specErr
	}
	return &engine.ExecutionSpec{
		Image:                 "test-image:latest",
		Command:               []string{"echo", "hello"},
		ActiveDeadlineSeconds: 3600,
	}, nil
}

func (m *recordingEngine) BuildPrompt(_ engine.Task) (string, error) { return "test", nil }
func (m *recordingEngine) Name() string                              { return m.name }
func (m *recordingEngine) InterfaceVersion() int                     { return 1 }

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

func TestProcessIncidentEvent(t *testing.T) {
	cfg := incidentTestConfig()
	cfg.IncidentTriage.AppendSystemPrompt = "Invoke /incident-classifier."
	logger := testLogger()
	k8s := fake.NewSimpleClientset()

	eng := &recordingEngine{name: "claude-code"}
	jb := &mockJobBuilder{}

	r := NewReconciler(cfg, logger,
		WithEngine(eng),
		WithJobBuilder(jb),
		WithK8sClient(k8s),
		WithNamespace("test-ns"),
	)

	ctx := context.Background()
	evt := incidentTestEvent("01HZINCIDENTABC")

	require.NoError(t, r.ProcessIncidentEvent(ctx, evt))

	// TaskRun must land in r.taskRuns under the incident:event_type key.
	tr, ok := r.GetTaskRun("01HZINCIDENTABC:" + webhook.EventIncidentCreatedV2)
	require.True(t, ok, "TaskRun missing from idempotency map")
	assert.Equal(t, taskrun.StateRunning, tr.State)
	assert.Equal(t, "claude-code", tr.Engine)
	assert.NotEmpty(t, tr.JobName)

	// Task fields populated from the IncidentEvent.
	assert.Equal(t, "01HZINCIDENTABC", eng.lastTask.ID)
	assert.Equal(t, "Database is down", eng.lastTask.Title)
	assert.Equal(t, "Customers reporting 500s", eng.lastTask.Description)
	assert.Equal(t, "https://app.incident.io/incidents/01HZINCIDENTABC", eng.lastTask.TicketURL)
	assert.Empty(t, eng.lastTask.RepoURL, "incident triage tasks must not carry a repo URL")
	assert.Contains(t, eng.lastTask.Labels, "osmia:source:incident-io")
	assert.Contains(t, eng.lastTask.Labels, "osmia:event:"+webhook.EventIncidentCreatedV2)

	// AppendSystemPrompt override propagates into the per-call EngineConfig.
	assert.Equal(t, "Invoke /incident-classifier.", eng.lastConfig.AppendSystemPrompt)

	// K8s Job must be created.
	jobs, err := k8s.BatchV1().Jobs("test-ns").List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	assert.Len(t, jobs.Items, 1)
}

func TestProcessIncidentEvent_Idempotency(t *testing.T) {
	cfg := incidentTestConfig()
	logger := testLogger()
	k8s := fake.NewSimpleClientset()

	eng := &recordingEngine{name: "claude-code"}
	jb := &mockJobBuilder{}

	r := NewReconciler(cfg, logger,
		WithEngine(eng),
		WithJobBuilder(jb),
		WithK8sClient(k8s),
		WithNamespace("test-ns"),
	)

	ctx := context.Background()
	evt := incidentTestEvent("01HZDUPLICATE")

	require.NoError(t, r.ProcessIncidentEvent(ctx, evt))
	require.NoError(t, r.ProcessIncidentEvent(ctx, evt))

	// Second call must be a no-op while the first is still running.
	jobs, err := k8s.BatchV1().Jobs("test-ns").List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	assert.Len(t, jobs.Items, 1, "duplicate event must not spawn a second job")
	assert.Equal(t, 1, eng.buildCalled, "BuildExecutionSpec must be called once")
}

func TestProcessIncidentEvent_DistinctEventTypes(t *testing.T) {
	// Same incident_id but different event_types must produce distinct
	// task runs — this is the whole reason the idempotency key includes
	// the event type.
	cfg := incidentTestConfig()
	logger := testLogger()
	k8s := fake.NewSimpleClientset()

	eng := &recordingEngine{name: "claude-code"}
	jb := &mockJobBuilder{}

	r := NewReconciler(cfg, logger,
		WithEngine(eng),
		WithJobBuilder(jb),
		WithK8sClient(k8s),
		WithNamespace("test-ns"),
	)

	ctx := context.Background()

	created := incidentTestEvent("01HZLIFECYCLE")
	require.NoError(t, r.ProcessIncidentEvent(ctx, created))

	statusUpdated := incidentTestEvent("01HZLIFECYCLE")
	statusUpdated.EventType = webhook.EventIncidentStatusUpdatedV2
	require.NoError(t, r.ProcessIncidentEvent(ctx, statusUpdated))

	jobs, err := k8s.BatchV1().Jobs("test-ns").List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	assert.Len(t, jobs.Items, 2, "created and status_updated must be distinct task runs")
	assert.Equal(t, 2, eng.buildCalled)

	_, ok := r.GetTaskRun("01HZLIFECYCLE:" + webhook.EventIncidentCreatedV2)
	assert.True(t, ok)
	_, ok = r.GetTaskRun("01HZLIFECYCLE:" + webhook.EventIncidentStatusUpdatedV2)
	assert.True(t, ok)
}

func TestProcessIncidentEvent_DefaultsEngineToClaudeCode(t *testing.T) {
	cfg := incidentTestConfig()
	cfg.IncidentTriage.Engine = "" // fall back to default
	logger := testLogger()
	k8s := fake.NewSimpleClientset()

	eng := &recordingEngine{name: "claude-code"}
	jb := &mockJobBuilder{}

	r := NewReconciler(cfg, logger,
		WithEngine(eng),
		WithJobBuilder(jb),
		WithK8sClient(k8s),
		WithNamespace("test-ns"),
	)

	ctx := context.Background()
	require.NoError(t, r.ProcessIncidentEvent(ctx, incidentTestEvent("01HZDEFAULT")))

	tr, ok := r.GetTaskRun("01HZDEFAULT:" + webhook.EventIncidentCreatedV2)
	require.True(t, ok)
	assert.Equal(t, "claude-code", tr.Engine)
}

func TestProcessIncidentEvent_EngineNotRegistered(t *testing.T) {
	cfg := incidentTestConfig()
	cfg.IncidentTriage.Engine = "definitely-not-an-engine"
	logger := testLogger()
	k8s := fake.NewSimpleClientset()

	eng := &recordingEngine{name: "claude-code"}
	jb := &mockJobBuilder{}

	r := NewReconciler(cfg, logger,
		WithEngine(eng),
		WithJobBuilder(jb),
		WithK8sClient(k8s),
		WithNamespace("test-ns"),
	)

	ctx := context.Background()
	err := r.ProcessIncidentEvent(ctx, incidentTestEvent("01HZNOENG"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "definitely-not-an-engine")
	assert.Contains(t, err.Error(), "not registered")
	assert.Equal(t, 0, eng.buildCalled)
}

func TestProcessIncidentEvent_BuildExecutionSpecError(t *testing.T) {
	cfg := incidentTestConfig()
	logger := testLogger()
	k8s := fake.NewSimpleClientset()

	eng := &recordingEngine{name: "claude-code", specErr: fmt.Errorf("engine boom")}
	jb := &mockJobBuilder{}

	r := NewReconciler(cfg, logger,
		WithEngine(eng),
		WithJobBuilder(jb),
		WithK8sClient(k8s),
		WithNamespace("test-ns"),
	)

	ctx := context.Background()
	err := r.ProcessIncidentEvent(ctx, incidentTestEvent("01HZBOOM"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "building execution spec")
	assert.Contains(t, err.Error(), "engine boom")

	jobs, err := k8s.BatchV1().Jobs("test-ns").List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	assert.Len(t, jobs.Items, 0, "failed BuildExecutionSpec must not create a job")
}

// TestProcessIncidentEvent_RelaunchAfterTerminal covers the
// idempotency-map fall-through branch: once an existing TaskRun for a
// given incident_id:event_type reaches a terminal state, a fresh event
// with the same key produces a new TaskRun and Job rather than no-opping
// against the terminated one. This mirrors ProcessTicket's behaviour and
// matters when incident.io re-delivers an event after the original run
// has completed.
func TestProcessIncidentEvent_RelaunchAfterTerminal(t *testing.T) {
	cfg := incidentTestConfig()
	logger := testLogger()
	k8s := fake.NewSimpleClientset()

	eng := &recordingEngine{name: "claude-code"}
	jb := &mockJobBuilder{}

	r := NewReconciler(cfg, logger,
		WithEngine(eng),
		WithJobBuilder(jb),
		WithK8sClient(k8s),
		WithNamespace("test-ns"),
	)

	ctx := context.Background()
	evt := incidentTestEvent("01HZTERMINAL")

	// First run lands the TaskRun in the idempotency map.
	require.NoError(t, r.ProcessIncidentEvent(ctx, evt))

	tr, ok := r.GetTaskRun("01HZTERMINAL:" + webhook.EventIncidentCreatedV2)
	require.True(t, ok)
	// Transition Running → Succeeded so IsTerminal() returns true.
	require.NoError(t, tr.Transition(taskrun.StateSucceeded))

	// Brief pause so the second call's TaskRun ID (which embeds
	// time.Now().UnixMilli()) differs from the first — otherwise the
	// derived K8s Job name collides and Create() fails with "already
	// exists". In production, two webhook deliveries colliding inside
	// the same millisecond is implausible.
	time.Sleep(2 * time.Millisecond)

	// Second call with the same key must NOT no-op now that the prior
	// run is terminal — it should build a fresh execution spec and job.
	require.NoError(t, r.ProcessIncidentEvent(ctx, evt))

	jobs, err := k8s.BatchV1().Jobs("test-ns").List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	assert.Len(t, jobs.Items, 2, "terminal prior run must allow re-launch")
	assert.Equal(t, 2, eng.buildCalled)
}
