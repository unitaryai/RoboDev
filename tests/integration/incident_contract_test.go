//go:build integration

// Package integration_test contains the cross-cutting incident-triage
// contract test: it exercises the full webhook-parse -> reconciler ->
// jobbuilder pipeline with the real claude-code engine and the real
// jobbuilder, so that the produced Kubernetes Job is asserted end to end
// rather than through recording fakes. These tests pin the incident-triage
// contract relied on by the internal FirstResponder deployment ahead of
// the use-case abstraction refactor described in internal/controller/incident.go.
package integration_test

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/unitaryai/osmia/internal/config"
	"github.com/unitaryai/osmia/internal/controller"
	"github.com/unitaryai/osmia/internal/jobbuilder"
	"github.com/unitaryai/osmia/internal/webhook"
	"github.com/unitaryai/osmia/pkg/engine/claudecode"
)

// incidentContractPayload is a realistic public_incident.incident_created_v2
// delivery, parsed the same way the /webhooks/incident-io HTTP handler
// parses it (see internal/webhook/incident_test.go for the HTTP-level
// signature-verification contract, which this test does not repeat).
const incidentContractPayload = `{
  "event_type": "public_incident.incident_created_v2",
  "public_incident.incident_created_v2": {
    "id": "01HZCONTRACT00000000000001",
    "reference": "INC-789",
    "name": "Payments API returning 500s",
    "summary": "Customers cannot check out",
    "permalink": "https://app.incident.io/incidents/789",
    "visibility": "public",
    "mode": "standard",
    "incident_status": {
      "id": "01HZ_status_triage",
      "name": "Triage",
      "category": "triage",
      "rank": 1
    },
    "slack_team_id": "T0123",
    "slack_channel_id": "C0123",
    "created_at": "2026-05-07T10:00:00Z",
    "updated_at": "2026-05-07T10:00:00Z"
  }
}`

// incidentContractConfig returns a config with every IncidentTriage field
// populated, mirroring a real FirstResponder deployment.
func incidentContractConfig() *config.Config {
	return &config.Config{
		Engines: config.EnginesConfig{Default: "claude-code"},
		GuardRails: config.GuardRailsConfig{
			MaxConcurrentJobs:     5,
			MaxJobDurationMinutes: 120,
		},
		IncidentTriage: config.IncidentTriageConfig{
			Engine:                 "claude-code",
			AppendSystemPrompt:     "Invoke /incident-classifier and do not clone a repository.",
			SlackChannelID:         "C_INCIDENT_TRIAGE",
			SlackTokenSecret:       "incident-slack-bot",
			IncidentIOAPIKeySecret: "incident-io-api",
		},
	}
}

// findEnvVar returns the EnvVar with the given name, or nil.
func findEnvVar(env []corev1.EnvVar, name string) *corev1.EnvVar {
	for i := range env {
		if env[i].Name == name {
			return &env[i]
		}
	}
	return nil
}

// TestIncidentContract_JobSpecCarriesAllConfiguredFields is the cross-
// cutting job-spec pin (contract pin 5): with every IncidentTriage field
// set, the Job built by the real claude-code engine + real jobbuilder
// carries SLACK_CHANNEL_ID as a plain env var, SecretKeyRefs for
// SLACK_BOT_TOKEN and INCIDENT_IO_API_KEY (the latter keyed literally
// "INCIDENT_IO_API_KEY"), and the configured AppendSystemPrompt reaches the
// claude CLI's --append-system-prompt argument.
func TestIncidentContract_JobSpecCarriesAllConfiguredFields(t *testing.T) {
	t.Parallel()

	cfg := incidentContractConfig()
	k8s := fake.NewSimpleClientset(
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "incident-slack-bot", Namespace: "test-ns"},
			Data:       map[string][]byte{"SLACK_BOT_TOKEN": []byte("xoxb-test")},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "incident-io-api", Namespace: "test-ns"},
			Data:       map[string][]byte{"INCIDENT_IO_API_KEY": []byte("inc_test_key")},
		},
	)

	eng := claudecode.New()
	jb := jobbuilder.NewJobBuilder("test-ns")
	logger := reconcilerTestLogger()

	r := controller.NewReconciler(cfg, logger,
		controller.WithEngine(eng),
		controller.WithJobBuilder(jb),
		controller.WithK8sClient(k8s),
		controller.WithNamespace("test-ns"),
	)

	evt, err := webhook.ParseIncidentEvent([]byte(incidentContractPayload))
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, r.ProcessIncidentEvent(ctx, evt))

	jobs, err := k8s.BatchV1().Jobs("test-ns").List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, jobs.Items, 1)

	job := jobs.Items[0]
	require.Len(t, job.Spec.Template.Spec.Containers, 1)
	container := job.Spec.Template.Spec.Containers[0]

	slackChannel := findEnvVar(container.Env, "SLACK_CHANNEL_ID")
	require.NotNil(t, slackChannel, "SLACK_CHANNEL_ID must be present on the agent container")
	assert.Equal(t, "C_INCIDENT_TRIAGE", slackChannel.Value)

	slackToken := findEnvVar(container.Env, "SLACK_BOT_TOKEN")
	require.NotNil(t, slackToken, "SLACK_BOT_TOKEN must be present on the agent container")
	require.NotNil(t, slackToken.ValueFrom)
	require.NotNil(t, slackToken.ValueFrom.SecretKeyRef)
	assert.Equal(t, "incident-slack-bot", slackToken.ValueFrom.SecretKeyRef.Name)
	assert.Equal(t, "SLACK_BOT_TOKEN", slackToken.ValueFrom.SecretKeyRef.Key)

	incidentIOKey := findEnvVar(container.Env, "INCIDENT_IO_API_KEY")
	require.NotNil(t, incidentIOKey, "INCIDENT_IO_API_KEY must be present on the agent container")
	require.NotNil(t, incidentIOKey.ValueFrom)
	require.NotNil(t, incidentIOKey.ValueFrom.SecretKeyRef)
	assert.Equal(t, "incident-io-api", incidentIOKey.ValueFrom.SecretKeyRef.Name)
	assert.Equal(t, "INCIDENT_IO_API_KEY", incidentIOKey.ValueFrom.SecretKeyRef.Key,
		"the INCIDENT_IO_API_KEY secret key is the literal env var name, unlike Slack's well-known-key probing")

	// The append_system_prompt value must reach the actual claude CLI
	// invocation as --append-system-prompt <value>.
	require.Contains(t, container.Command, "--append-system-prompt")
	idx := indexOf(container.Command, "--append-system-prompt")
	require.GreaterOrEqual(t, idx, 0)
	require.Less(t, idx+1, len(container.Command))
	assert.Equal(t, "Invoke /incident-classifier and do not clone a repository.", container.Command[idx+1])
}

// indexOf returns the index of needle in haystack, or -1.
func indexOf(haystack []string, needle string) int {
	for i, s := range haystack {
		if s == needle {
			return i
		}
	}
	return -1
}

// TestIncidentContract_SharedEngineSkillsReachIncidentJob pins contract pin
// 12: skills configured on the shared claude-code engine instance (as
// engines.claude_code.skills would configure it) reach the incident-triage
// agent Job's environment as CLAUDE_SKILL_* variables, exactly as they
// would for a ticketing job — the skill wiring lives on the engine, not in
// baseEngineConfig, so it is shared automatically across both flows.
func TestIncidentContract_SharedEngineSkillsReachIncidentJob(t *testing.T) {
	t.Parallel()

	cfg := incidentContractConfig()
	k8s := fake.NewSimpleClientset()

	eng := claudecode.New(claudecode.WithSkills([]claudecode.Skill{
		{Name: "incident-classifier", Inline: "# Incident Classifier\n\nClassify the incident."},
	}))
	jb := jobbuilder.NewJobBuilder("test-ns")
	logger := reconcilerTestLogger()

	r := controller.NewReconciler(cfg, logger,
		controller.WithEngine(eng),
		controller.WithJobBuilder(jb),
		controller.WithK8sClient(k8s),
		controller.WithNamespace("test-ns"),
	)

	evt, err := webhook.ParseIncidentEvent([]byte(incidentContractPayload))
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, r.ProcessIncidentEvent(ctx, evt))

	jobs, err := k8s.BatchV1().Jobs("test-ns").List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, jobs.Items, 1)

	container := jobs.Items[0].Spec.Template.Spec.Containers[0]
	skillVar := findEnvVar(container.Env, "CLAUDE_SKILL_INLINE_INCIDENT_CLASSIFIER")
	require.NotNil(t, skillVar, "shared-engine skills must produce CLAUDE_SKILL_* env vars on incident job specs")
	assert.NotEmpty(t, skillVar.Value)
}

// TestIncidentContract_TaskRunAndJobNamingIsStable pins contract pin 8 at
// the cross-cutting level: the TaskRun ID surfaces as the Job name prefix
// and as a K8s label, and both are well-formed for a real-shaped incident
// ID and event type.
func TestIncidentContract_TaskRunAndJobNamingIsStable(t *testing.T) {
	t.Parallel()

	cfg := incidentContractConfig()
	k8s := fake.NewSimpleClientset()

	eng := claudecode.New()
	jb := jobbuilder.NewJobBuilder("test-ns")
	logger := reconcilerTestLogger()

	r := controller.NewReconciler(cfg, logger,
		controller.WithEngine(eng),
		controller.WithJobBuilder(jb),
		controller.WithK8sClient(k8s),
		controller.WithNamespace("test-ns"),
	)

	evt, err := webhook.ParseIncidentEvent([]byte(incidentContractPayload))
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, r.ProcessIncidentEvent(ctx, evt))

	tr, ok := r.GetTaskRun(evt.Incident.ID + ":" + webhook.EventIncidentCreatedV2)
	require.True(t, ok)

	jobs, err := k8s.BatchV1().Jobs("test-ns").List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, jobs.Items, 1)

	assert.Equal(t, tr.JobName, jobs.Items[0].Name)
	assert.Contains(t, jobs.Items[0].Name, "osmia-tr-incident-")
	assert.Equal(t, tr.ID, jobs.Items[0].Labels[jobbuilder.LabelTaskRunID])
	assert.LessOrEqual(t, len(jobs.Items[0].Name), 63)
}
