package controller

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/unitaryai/osmia/internal/config"
	"github.com/unitaryai/osmia/internal/estimator"
	"github.com/unitaryai/osmia/internal/memory"
	"github.com/unitaryai/osmia/internal/reviewpoller"
	"github.com/unitaryai/osmia/internal/taskrun"
	"github.com/unitaryai/osmia/internal/tournament"
	"github.com/unitaryai/osmia/internal/webhook"
	"github.com/unitaryai/osmia/pkg/engine"
	"github.com/unitaryai/osmia/pkg/plugin/notifications"
	"github.com/unitaryai/osmia/pkg/plugin/ticketing"
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

// TestProcessIncidentEvent_SetsIncidentIOAPIKeySecretKeyRef verifies that
// the incident-triage agent Job's INCIDENT_IO_API_KEY SecretKeyRef points
// at the Secret named by IncidentTriage.IncidentIOAPIKeySecret. The Key
// is the literal "INCIDENT_IO_API_KEY" — unlike the Slack token there is
// no well-known-key probing because the operator-managed Secret is
// expected to follow the documented naming convention.
func TestProcessIncidentEvent_SetsIncidentIOAPIKeySecretKeyRef(t *testing.T) {
	cfg := incidentTestConfigWithTicketingSlack()
	cfg.IncidentTriage.IncidentIOAPIKeySecret = "incident-io-api"

	logger := testLogger()
	k8s := fake.NewSimpleClientset(
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "incident-io-api",
				Namespace: "test-ns",
			},
			Data: map[string][]byte{
				"INCIDENT_IO_API_KEY": []byte("inc_test_key"),
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

	require.NoError(t, r.ProcessIncidentEvent(context.Background(), incidentTestEvent("01HZINCIOAPI")))

	ref, ok := eng.lastConfig.SecretKeyRefs["INCIDENT_IO_API_KEY"]
	require.True(t, ok, "INCIDENT_IO_API_KEY SecretKeyRef must be set on the agent Job")
	assert.Equal(t, "incident-io-api", ref.SecretName,
		"SecretName must point at IncidentTriage.IncidentIOAPIKeySecret")
	assert.Equal(t, "INCIDENT_IO_API_KEY", ref.Key,
		"Key is the literal env var name — no well-known-key probing for this field")
}

// TestProcessIncidentEvent_OmitsIncidentIOAPIKeyWhenSecretNotConfigured
// verifies that when IncidentTriage.IncidentIOAPIKeySecret is empty the
// agent Job receives no INCIDENT_IO_API_KEY SecretKeyRef. This is the
// path setup-claude.sh relies on to skip MCP server registration for
// flows that don't need incident.io access.
func TestProcessIncidentEvent_OmitsIncidentIOAPIKeyWhenSecretNotConfigured(t *testing.T) {
	cfg := incidentTestConfigWithTicketingSlack()
	// IncidentIOAPIKeySecret deliberately left empty.

	logger := testLogger()
	eng := &recordingEngine{name: "claude-code"}
	jb := &mockJobBuilder{}
	r := NewReconciler(cfg, logger,
		WithEngine(eng),
		WithJobBuilder(jb),
		WithNamespace("test-ns"),
	)

	require.NoError(t, r.ProcessIncidentEvent(context.Background(), incidentTestEvent("01HZINCIOOFF")))

	_, ok := eng.lastConfig.SecretKeyRefs["INCIDENT_IO_API_KEY"]
	assert.False(t, ok,
		"INCIDENT_IO_API_KEY SecretKeyRef must be absent when IncidentIOAPIKeySecret is empty")
}

// TestProcessIncidentEvent_TaskRunIDIsDNS1123AndLabelSafe pins the
// tr-incident-<lower(id)>-<created|updated|evt>-<ms> TaskRun ID shape
// documented on eventTypeSuffix's doc comment, and verifies it stays within
// the Kubernetes label-value limit for a realistic incident.io ID:
// incident.io issues 26-character ULIDs, which is the size this format was
// designed around.
func TestProcessIncidentEvent_TaskRunIDIsDNS1123AndLabelSafe(t *testing.T) {
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

	// A realistic, mixed-case 26-character ULID, matching incident.io's
	// documented ID format. Mixed case exercises the lowercasing step.
	incidentID := "01HZqJ8k3M5n7P9r2T4v6X8y0Z"
	require.Len(t, incidentID, 26)

	ctx := context.Background()
	require.NoError(t, r.ProcessIncidentEvent(ctx, incidentTestEvent(incidentID)))

	tr, ok := r.GetTaskRun(incidentID + ":" + webhook.EventIncidentCreatedV2)
	require.True(t, ok)

	trIDPattern := regexp.MustCompile(`^tr-incident-[0-9a-z]+-(created|updated|evt)-\d+$`)
	assert.Regexp(t, trIDPattern, tr.ID,
		"TaskRun ID must follow the documented tr-incident-<lower(id)>-<suffix>-<ms> shape")
	assert.Equal(t, strings.ToLower(tr.ID), tr.ID, "TaskRun ID must be all-lowercase")

	// tr.ID is used verbatim as the osmia.io/task-run-id label value on both
	// the Job and its Pod template (jobbuilder.LabelTaskRunID). Unlike the
	// Job *name*, which jobbuilder truncates to 63 characters, the label
	// value is not truncated — a label value over 63 characters, or one
	// that fails Kubernetes' label-value syntax, would be silently rejected
	// by a real API server (the fake clientset used in these tests does not
	// validate this).
	assert.LessOrEqual(t, len(tr.ID), 63,
		"TaskRun ID doubles as a K8s label value, which is capped at 63 characters")
	assert.Empty(t, validation.IsValidLabelValue(tr.ID),
		"TaskRun ID must be a syntactically valid Kubernetes label value")
}

// spyEngineSelector implements EngineSelector and records every call, so
// tests can assert that a code path never consults the ticketing flow's
// fallback-chain engine selection logic.
type spyEngineSelector struct {
	calls int
}

func (s *spyEngineSelector) SelectEngines(_ ticketing.Ticket) []string {
	s.calls++
	return []string{"claude-code"}
}

// spyNotifier implements notifications.Channel and records NotifyStart
// calls, so tests can assert that a code path never sends a ticketing-style
// start notification (the incident-triage flow relies on the agent's own
// MCP Slack posting instead).
type spyNotifier struct {
	notifyStartCalls int
}

func (s *spyNotifier) Notify(_ context.Context, _ string, _ ticketing.Ticket, _ string) error {
	return nil
}

func (s *spyNotifier) NotifyStart(_ context.Context, _ ticketing.Ticket) (string, error) {
	s.notifyStartCalls++
	return "thread-ref", nil
}

func (s *spyNotifier) NotifyComplete(_ context.Context, _ ticketing.Ticket, _ engine.TaskResult, _ string) error {
	return nil
}

func (s *spyNotifier) UpdateMessage(_ context.Context, _ string, _ string) error { return nil }
func (s *spyNotifier) Name() string                                              { return "spy-notifier" }
func (s *spyNotifier) InterfaceVersion() int                                     { return notifications.InterfaceVersion }

// TestProcessIncidentEvent_SkipsEngineSelector pins that incident triage
// picks its engine directly from IncidentTriage.Engine and never consults
// the ticketing flow's fallback-chain EngineSelector.
func TestProcessIncidentEvent_SkipsEngineSelector(t *testing.T) {
	cfg := incidentTestConfig()
	logger := testLogger()
	k8s := fake.NewSimpleClientset()

	eng := &recordingEngine{name: "claude-code"}
	jb := &mockJobBuilder{}
	selector := &spyEngineSelector{}

	r := NewReconciler(cfg, logger,
		WithEngine(eng),
		WithJobBuilder(jb),
		WithK8sClient(k8s),
		WithNamespace("test-ns"),
		WithEngineSelector(selector),
	)

	require.NoError(t, r.ProcessIncidentEvent(context.Background(), incidentTestEvent("01HZSELECTOR")))

	assert.Equal(t, 0, selector.calls,
		"incident triage must never consult the fallback-chain EngineSelector")
}

// TestProcessIncidentEvent_SkipsCostEstimation pins that incident triage
// never runs the predictive cost/duration estimation gate. A near-zero
// budget guarantees ShouldAutoReject would trigger on the cold-start
// default cost range (USD 1-5) if the estimator were ever consulted for
// this flow.
func TestProcessIncidentEvent_SkipsCostEstimation(t *testing.T) {
	cfg := incidentTestConfig()
	cfg.Estimator.MaxPredictedCostPerJob = 0.01
	logger := testLogger()
	k8s := fake.NewSimpleClientset()

	eng := &recordingEngine{name: "claude-code"}
	jb := &mockJobBuilder{}
	predictor := estimator.NewPredictor(estimator.NewMemoryEstimatorStore(), &cfg.Estimator, logger)
	scorer := estimator.NewComplexityScorer()

	r := NewReconciler(cfg, logger,
		WithEngine(eng),
		WithJobBuilder(jb),
		WithK8sClient(k8s),
		WithNamespace("test-ns"),
		WithEstimator(predictor, scorer),
	)

	require.NoError(t, r.ProcessIncidentEvent(context.Background(), incidentTestEvent("01HZCOST")))

	tr, ok := r.GetTaskRun("01HZCOST:" + webhook.EventIncidentCreatedV2)
	require.True(t, ok)
	assert.Equal(t, taskrun.StateRunning, tr.State,
		"a budget that would auto-reject any ticket must not affect incident triage: cost estimation is ticketing-only")

	jobs, err := k8s.BatchV1().Jobs("test-ns").List(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)
	assert.Len(t, jobs.Items, 1)
}

// TestProcessIncidentEvent_SkipsTournament pins that incident triage never
// fans out into a competitive-execution tournament, even when
// CompetitiveExecution is enabled with >=2 candidates and >=2 engines are
// registered — the conditions that would trigger launchTournament in
// ProcessTicket.
func TestProcessIncidentEvent_SkipsTournament(t *testing.T) {
	cfg := incidentTestConfig()
	cfg.CompetitiveExecution = config.CompetitiveExecutionConfig{
		Enabled:           true,
		DefaultCandidates: 2,
	}
	logger := testLogger()
	k8s := fake.NewSimpleClientset()

	eng := &recordingEngine{name: "claude-code"}
	eng2 := &recordingEngine{name: "codex"}
	jb := &mockJobBuilder{}
	coord := tournament.NewCoordinator(logger)

	r := NewReconciler(cfg, logger,
		WithEngine(eng),
		WithEngine(eng2),
		WithJobBuilder(jb),
		WithK8sClient(k8s),
		WithNamespace("test-ns"),
		WithTournamentCoordinator(coord),
	)

	require.NoError(t, r.ProcessIncidentEvent(context.Background(), incidentTestEvent("01HZTOURNEY")))

	jobs, err := k8s.BatchV1().Jobs("test-ns").List(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)
	assert.Len(t, jobs.Items, 1,
		"incident triage must produce a single job, never a tournament fan-out")
	assert.Equal(t, 1, eng.buildCalled)
	assert.Equal(t, 0, eng2.buildCalled, "the second registered engine must never be consulted by incident triage")
}

// TestProcessIncidentEvent_SkipsApprovalGate pins that a configured
// pre_start approval gate — which holds ProcessTicket's TaskRun in
// NeedsHuman — has no effect on incident triage.
func TestProcessIncidentEvent_SkipsApprovalGate(t *testing.T) {
	cfg := incidentTestConfig()
	cfg.GuardRails.ApprovalGates = []string{"pre_start"}
	logger := testLogger()
	k8s := fake.NewSimpleClientset()

	eng := &recordingEngine{name: "claude-code"}
	jb := &mockJobBuilder{}
	approvalBackend := &stubApprovalBackend{}

	r := NewReconciler(cfg, logger,
		WithEngine(eng),
		WithJobBuilder(jb),
		WithK8sClient(k8s),
		WithNamespace("test-ns"),
		WithApprovalBackend(approvalBackend),
	)

	require.NoError(t, r.ProcessIncidentEvent(context.Background(), incidentTestEvent("01HZAPPROVAL")))

	tr, ok := r.GetTaskRun("01HZAPPROVAL:" + webhook.EventIncidentCreatedV2)
	require.True(t, ok)
	assert.Equal(t, taskrun.StateRunning, tr.State,
		"a configured pre_start approval gate must not hold incident-triage runs")
	assert.Empty(t, approvalBackend.requests, "no approval request should be sent for an incident-triage run")
}

// TestProcessIncidentEvent_SkipsMemoryQuery pins that incident-triage tasks
// never carry MemoryContext, even when episodic memory holds a relevant
// prior fact — the memory query gate is ticketing-only.
func TestProcessIncidentEvent_SkipsMemoryQuery(t *testing.T) {
	cfg := incidentTestConfig()
	logger := testLogger()
	k8s := fake.NewSimpleClientset()

	graph := memory.NewGraph(nil, logger)
	ctx := context.Background()
	require.NoError(t, graph.AddNode(ctx, &memory.Fact{
		ID:         "incident-fact",
		Content:    "known issue with the database connection pool",
		FactKind:   memory.FactTypeFailurePattern,
		Confidence: 0.9,
		DecayRate:  0.01,
		ValidFrom:  time.Now(),
	}))
	extractor := memory.NewExtractor(logger)
	qe := memory.NewQueryEngine(graph, logger)

	eng := &recordingEngine{name: "claude-code"}
	jb := &mockJobBuilder{}

	r := NewReconciler(cfg, logger,
		WithEngine(eng),
		WithJobBuilder(jb),
		WithK8sClient(k8s),
		WithNamespace("test-ns"),
		WithMemory(graph, extractor, qe),
	)

	require.NoError(t, r.ProcessIncidentEvent(ctx, incidentTestEvent("01HZMEMORY")))

	assert.Empty(t, eng.lastTask.MemoryContext,
		"incident-triage tasks must never carry MemoryContext, even when episodic memory holds relevant facts")
}

// TestProcessIncidentEvent_SkipsNotifyStartAndMarkInProgress pins two
// ticketing-only side effects that ProcessTicket performs before launching
// a job: runNotifyStart (agent-authored MCP Slack posting replaces it for
// incident triage) and ticketing.MarkInProgress (no ticketing backend knows
// about incident.io IDs).
func TestProcessIncidentEvent_SkipsNotifyStartAndMarkInProgress(t *testing.T) {
	cfg := incidentTestConfig()
	logger := testLogger()
	k8s := fake.NewSimpleClientset()

	eng := &recordingEngine{name: "claude-code"}
	jb := &mockJobBuilder{}
	tb := newMockTicketing(nil)
	notifier := &spyNotifier{}

	r := NewReconciler(cfg, logger,
		WithEngine(eng),
		WithJobBuilder(jb),
		WithK8sClient(k8s),
		WithNamespace("test-ns"),
		WithTicketing(tb),
		WithNotifier(notifier),
	)

	require.NoError(t, r.ProcessIncidentEvent(context.Background(), incidentTestEvent("01HZNONOTIFY")))

	assert.Equal(t, 0, notifier.notifyStartCalls,
		"the incident agent posts to Slack itself via MCP; the reconciler must not also call NotifyStart")
	assert.Empty(t, tb.markedProgress,
		"no ticketing backend knows about incident IDs; MarkInProgress must not be called for incident triage runs")

	tr, ok := r.GetTaskRun("01HZNONOTIFY:" + webhook.EventIncidentCreatedV2)
	require.True(t, ok)
	assert.Empty(t, tr.NotificationThreadRef,
		"incident-triage TaskRuns must not carry a notification thread ref sourced from runNotifyStart")
}

// TestHandleJobComplete_IncidentTaskRun_ToleratesTicketingMarkCompleteError
// pins a currently-tolerated wart: handleJobComplete is shared between the
// ticketing (ProcessTicket) and incident-triage (ProcessIncidentEvent)
// flows. For an incident TaskRun, tr.TicketID is the incident.io incident
// ID, which the configured ticketing backend does not recognise —
// MarkComplete is expected to return a "not found"-shaped error in
// production. The error is logged but non-fatal: the TaskRun still
// transitions to Succeeded. See the "Known limitation" comment on
// ProcessIncidentEvent — the clean fix needs the use-case-aware
// completion-handler dispatch that the upcoming abstraction will provide.
func TestHandleJobComplete_IncidentTaskRun_ToleratesTicketingMarkCompleteError(t *testing.T) {
	logger := testLogger()
	tb := newMockTicketing(nil)
	tb.markCompleteErr = fmt.Errorf("ticket 01HZCOMPLETE not found")

	tr := taskrun.New("tr-incident-01hzcomplete-created-1", "01HZCOMPLETE:"+webhook.EventIncidentCreatedV2, "01HZCOMPLETE", "claude-code")
	_ = tr.Transition(taskrun.StateRunning)

	r := &Reconciler{
		config:       incidentTestConfig(),
		logger:       logger,
		ticketing:    tb,
		taskRuns:     map[string]*taskrun.TaskRun{tr.IdempotencyKey: tr},
		taskRunStore: taskrun.NewMemoryStore(),
	}

	r.handleJobComplete(context.Background(), tr)

	assert.Equal(t, taskrun.StateSucceeded, tr.State,
		"MarkComplete failing for an unrecognised incident ID must not prevent the TaskRun reaching Succeeded")
	assert.Contains(t, tb.markedComplete, "01HZCOMPLETE")
}

// TestHandleJobComplete_IncidentTaskRun_SkipsReviewPollerRegistration pins
// the guard in handleJobComplete that only calls reviewPoller.Register when
// result.MergeRequestURL is non-empty — incident-triage runs never produce
// a merge request. reviewpoller.Poller exposes no accessor for its internal
// tracked-PR state, so this test asserts the observable half of the
// contract (handleJobComplete completes cleanly, reaching Succeeded, with a
// reviewPoller configured and an empty MergeRequestURL); it cannot directly
// assert that Register was never invoked without either a production
// change (an exported query method on Poller) or reflection into an
// unexported field, both out of scope for this test-only PR.
func TestHandleJobComplete_IncidentTaskRun_SkipsReviewPollerRegistration(t *testing.T) {
	logger := testLogger()
	tb := newMockTicketing(nil)
	poller := reviewpoller.New(config.ReviewResponseConfig{}, nil, logger)

	tr := taskrun.New("tr-incident-01hzreview-created-1", "01HZREVIEW:"+webhook.EventIncidentCreatedV2, "01HZREVIEW", "claude-code")
	_ = tr.Transition(taskrun.StateRunning)

	r := &Reconciler{
		config:       incidentTestConfig(),
		logger:       logger,
		ticketing:    tb,
		taskRuns:     map[string]*taskrun.TaskRun{tr.IdempotencyKey: tr},
		taskRunStore: taskrun.NewMemoryStore(),
		reviewPoller: poller,
	}

	r.handleJobComplete(context.Background(), tr)

	assert.Equal(t, taskrun.StateSucceeded, tr.State)
	require.NotNil(t, tr.Result)
	assert.Empty(t, tr.Result.MergeRequestURL,
		"the generic-success fallback result must not carry a merge request URL for an incident run")
}

// TestProcessIncidentEvent_StartsStreamReaderOnlyForClaudeCode pins that
// the real-time NDJSON stream reader is only started when the dispatched
// engine is claude-code, making explicit what was previously an implicit
// side effect of the `engineName == defaultIncidentEngine` check in
// ProcessIncidentEvent.
func TestProcessIncidentEvent_StartsStreamReaderOnlyForClaudeCode(t *testing.T) {
	tests := []struct {
		name       string
		engineName string
		wantReader bool
	}{
		{name: "claude-code starts the stream reader", engineName: "claude-code", wantReader: true},
		{name: "non-claude-code engine does not start the stream reader", engineName: "codex", wantReader: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := incidentTestConfig()
			cfg.IncidentTriage.Engine = tt.engineName
			logger := testLogger()
			k8s := fake.NewSimpleClientset()

			eng := &recordingEngine{name: tt.engineName}
			jb := &mockJobBuilder{}

			r := NewReconciler(cfg, logger,
				WithEngine(eng),
				WithJobBuilder(jb),
				WithK8sClient(k8s),
				WithNamespace("test-ns"),
			)

			incidentID := "01HZSTREAM" + strings.ToUpper(tt.engineName)
			require.NoError(t, r.ProcessIncidentEvent(context.Background(), incidentTestEvent(incidentID)))

			tr, ok := r.GetTaskRun(incidentID + ":" + webhook.EventIncidentCreatedV2)
			require.True(t, ok)

			r.mu.RLock()
			_, hasReader := r.streamReaders[tr.ID]
			r.mu.RUnlock()

			assert.Equal(t, tt.wantReader, hasReader,
				"stream reader presence must match whether the dispatched engine is claude-code")
		})
	}
}
