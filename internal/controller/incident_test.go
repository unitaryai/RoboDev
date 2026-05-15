package controller

import (
	"context"
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
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
// public_incident.incident_created_v2 path. IncidentStatus.Category and
// Mode are populated with realistic enum values so the predictable-
// fields-through-to-labels behaviour exercises in tests that don't
// otherwise customise the incident.
func incidentTestEvent(id string) webhook.IncidentEvent {
	return webhook.IncidentEvent{
		EventType: webhook.EventIncidentCreatedV2,
		Incident: webhook.IncidentV2{
			ID:        id,
			Reference: "INC-" + id,
			Name:      "Database is down",
			Summary:   "Customers reporting 500s",
			Permalink: "https://app.incident.io/incidents/" + id,
			Mode:      "standard",
			IncidentStatus: webhook.IncidentStatusV2{
				Category: "triage",
			},
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
	// Predictable, enum-like incident.io fields are surfaced verbatim
	// as labels so the classifier can reason over them without
	// having to parse the user-prompt prose.
	assert.Contains(t, eng.lastTask.Labels, "osmia:incident-status:triage")
	assert.Contains(t, eng.lastTask.Labels, "osmia:mode:standard")
	assert.Contains(t, eng.lastTask.Labels, "osmia:incident-reference:INC-01HZINCIDENTABC")

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

// incidentTestConfigWithTicketingSlack returns a config with one
// configured Slack notifications channel — the channel that slackEnv()
// and slackSecretKeyRefs() pick for the ticketing flow. Used by the
// IncidentTriage Slack-config tests to verify that the incident flow's
// own channel + token settings take effect when set, and that the
// ticketing channel is the fallback when they're not.
func incidentTestConfigWithTicketingSlack() *config.Config {
	cfg := incidentTestConfig()
	cfg.Notifications = config.NotificationsConfig{
		Channels: []config.ChannelConfig{
			{
				Backend: "slack",
				Config: map[string]any{
					"channel_id":   "C_TICKETING",
					"token_secret": "ticketing-bot",
				},
			},
		},
	}
	return cfg
}

// TestProcessIncidentEvent_PostsToConfiguredSlackChannel covers the
// happy path for per-flow Slack channel config: when
// IncidentTriage.SlackChannelID is set, the incident-triage agent Job's
// SLACK_CHANNEL_ID env var carries that channel rather than the
// ticketing channel from Notifications.Channels.
func TestProcessIncidentEvent_PostsToConfiguredSlackChannel(t *testing.T) {
	cfg := incidentTestConfigWithTicketingSlack()
	cfg.IncidentTriage.SlackChannelID = "C_INCIDENT"

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

	require.NoError(t, r.ProcessIncidentEvent(context.Background(), incidentTestEvent("01HZSLACKCH")))

	assert.Equal(t, "C_INCIDENT", eng.lastConfig.Env["SLACK_CHANNEL_ID"],
		"incident-triage agent Job must carry the incident flow's SLACK_CHANNEL_ID")
}

// TestProcessIncidentEvent_FallsBackToTicketingSlackChannel guards the
// backward-compatibility path: when IncidentTriage.SlackChannelID is
// unset, the incident flow shares the ticketing channel rather than
// failing outright. Keeps existing single-channel deployments working.
func TestProcessIncidentEvent_FallsBackToTicketingSlackChannel(t *testing.T) {
	cfg := incidentTestConfigWithTicketingSlack()
	// IncidentTriage.SlackChannelID intentionally left empty.

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

	require.NoError(t, r.ProcessIncidentEvent(context.Background(), incidentTestEvent("01HZSLACKDEF")))

	assert.Equal(t, "C_TICKETING", eng.lastConfig.Env["SLACK_CHANNEL_ID"],
		"with no per-flow channel set, the incident flow inherits the ticketing channel")
}

// TestProcessIncidentEvent_UsesConfiguredSlackTokenSecret covers the
// per-flow Slack bot path: when IncidentTriage.SlackTokenSecret is set,
// the incident-triage agent Job's SLACK_BOT_TOKEN SecretKeyRef points
// at that Secret. The well-known-key probe is exercised by seeding the
// fake clientset with a Secret that has a SLACK_BOT_TOKEN data key.
func TestProcessIncidentEvent_UsesConfiguredSlackTokenSecret(t *testing.T) {
	cfg := incidentTestConfigWithTicketingSlack()
	cfg.IncidentTriage.SlackTokenSecret = "incident-bot"

	logger := testLogger()
	k8s := fake.NewSimpleClientset(
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "incident-bot",
				Namespace: "test-ns",
			},
			Data: map[string][]byte{
				"SLACK_BOT_TOKEN": []byte("xoxb-incident-test"),
			},
		},
	)
	eng := &recordingEngine{name: "claude-code"}
	jb := &mockJobBuilder{}
	r := NewReconciler(cfg, logger,
		WithEngine(eng),
		WithJobBuilder(jb),
		WithK8sClient(k8s),
		WithNamespace("test-ns"),
	)

	require.NoError(t, r.ProcessIncidentEvent(context.Background(), incidentTestEvent("01HZSLACKTOK")))

	ref, ok := eng.lastConfig.SecretKeyRefs["SLACK_BOT_TOKEN"]
	require.True(t, ok, "SLACK_BOT_TOKEN SecretKeyRef must be set on the agent Job")
	assert.Equal(t, "incident-bot", ref.SecretName,
		"SecretName must point at IncidentTriage.SlackTokenSecret")
	assert.Equal(t, "SLACK_BOT_TOKEN", ref.Key,
		"resolveSlackTokenKey must probe the configured Secret and pick the well-known key")
}

// TestProcessIncidentEvent_FallsBackToTokenKeyForUnnamedSlackSecret
// covers the case where the configured Secret holds the token under the
// literal "token" key rather than a well-known name. resolveSlackTokenKey
// must fall through to "token", matching slackSecretKeyRefs's default.
func TestProcessIncidentEvent_FallsBackToTokenKeyForUnnamedSlackSecret(t *testing.T) {
	cfg := incidentTestConfigWithTicketingSlack()
	cfg.IncidentTriage.SlackTokenSecret = "incident-bot"

	logger := testLogger()
	k8s := fake.NewSimpleClientset(
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "incident-bot",
				Namespace: "test-ns",
			},
			Data: map[string][]byte{
				"token": []byte("xoxb-incident-test"),
			},
		},
	)
	eng := &recordingEngine{name: "claude-code"}
	jb := &mockJobBuilder{}
	r := NewReconciler(cfg, logger,
		WithEngine(eng),
		WithJobBuilder(jb),
		WithK8sClient(k8s),
		WithNamespace("test-ns"),
	)

	require.NoError(t, r.ProcessIncidentEvent(context.Background(), incidentTestEvent("01HZSLACKFB")))

	ref, ok := eng.lastConfig.SecretKeyRefs["SLACK_BOT_TOKEN"]
	require.True(t, ok)
	assert.Equal(t, "incident-bot", ref.SecretName)
	assert.Equal(t, "token", ref.Key,
		"with no well-known key in the Secret, the key falls back to the literal \"token\"")
}

// TestProcessIncidentEvent_AppendsUnderlyingAlertToDescription covers the
// happy path for surfacing alert-driven incidents: when Creator.Alert is
// populated, the agent's Task.Description gets the incident summary
// followed by an "Underlying alert" prose block carrying the alert's
// title and ID. Lets the skill answer context.underlying_alert directly
// rather than inferring it from the summary text.
func TestProcessIncidentEvent_AppendsUnderlyingAlertToDescription(t *testing.T) {
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

	evt := incidentTestEvent("01HZALERT")
	evt.Incident.Creator = &webhook.CreatorV2{
		Alert: &webhook.CreatorAlertV2{
			ID:    "01HZ_alert_db",
			Title: "Postgres replica lag exceeded 60s",
		},
	}

	require.NoError(t, r.ProcessIncidentEvent(context.Background(), evt))

	assert.Contains(t, eng.lastTask.Description, "Customers reporting 500s",
		"original summary must be preserved at the top of the description")
	assert.Contains(t, eng.lastTask.Description, "## Underlying alert",
		"alert-driven incidents must surface the underlying alert in a labelled block")
	assert.Contains(t, eng.lastTask.Description, "Postgres replica lag exceeded 60s")
	assert.Contains(t, eng.lastTask.Description, "alert id: 01HZ_alert_db")
}

// TestProcessIncidentEvent_OmitsUnderlyingAlertWhenCreatorNotAlertDriven
// guards the negative case: when the incident isn't alert-driven (no
// Creator at all, or Creator with an empty Alert pointer), the
// description stays as the bare summary — no spurious "Underlying alert"
// block appears.
func TestProcessIncidentEvent_OmitsUnderlyingAlertWhenCreatorNotAlertDriven(t *testing.T) {
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

	evt := incidentTestEvent("01HZNOALERT")
	// Creator left as nil (user-driven, webhook-driven, manual — none
	// of which surface an alert object).

	require.NoError(t, r.ProcessIncidentEvent(context.Background(), evt))

	assert.Equal(t, "Customers reporting 500s", eng.lastTask.Description,
		"description must remain the bare summary when no creator alert is present")
	assert.NotContains(t, eng.lastTask.Description, "Underlying alert")
}

// TestProcessIncidentEvent_OmitsEmptyIncidentLabels guards label-emission
// against zero-valued fields: an incident with empty Mode, Reference, and
// IncidentStatus.Category should not produce empty `osmia:mode:`,
// `osmia:incident-reference:`, or `osmia:incident-status:` labels.
func TestProcessIncidentEvent_OmitsEmptyIncidentLabels(t *testing.T) {
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

	// Strip the predictable fields the helper otherwise populates.
	evt := incidentTestEvent("01HZMINIMAL")
	evt.Incident.Mode = ""
	evt.Incident.Reference = ""
	evt.Incident.IncidentStatus = webhook.IncidentStatusV2{}

	require.NoError(t, r.ProcessIncidentEvent(context.Background(), evt))

	// Source + event labels always present.
	assert.Contains(t, eng.lastTask.Labels, "osmia:source:incident-io")
	assert.Contains(t, eng.lastTask.Labels, "osmia:event:"+webhook.EventIncidentCreatedV2)

	for _, label := range eng.lastTask.Labels {
		assert.NotEqual(t, "osmia:mode:", label,
			"empty Mode must not produce a dangling osmia:mode: label")
		assert.NotEqual(t, "osmia:incident-reference:", label,
			"empty Reference must not produce a dangling osmia:incident-reference: label")
		assert.NotEqual(t, "osmia:incident-status:", label,
			"empty IncidentStatus.Category must not produce a dangling osmia:incident-status: label")
	}
}
