package claudecode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/unitaryai/osmia/pkg/engine"
)

// This file pins the byte-identical BuildExecutionSpec and BuildPrompt output
// of ClaudeCodeEngine for five representative task shapes, ahead of
// refactor(engine): gate stream reading on a StreamEmitter capability
// instead of engine name. That refactor only adds a new StreamFormat method
// to ClaudeCodeEngine and switches the controller's stream-reader gates from
// an engine-name string comparison to a type assertion — it must not change
// a single byte of what BuildExecutionSpec or BuildPrompt produce. These
// goldens were captured before that refactor and are asserted to be
// unchanged after it; see TestBuildPrompt_IncidentTriageGolden above for the
// established convention (updateGolden flag, testdata/ layout) that this
// file follows.
//
// Regenerate with:
//
//	go test ./pkg/engine/claudecode/... -run TestGolden -update

// freshTicketingTask returns a fresh ticketing task with RepoURL set and no
// session, prior-MR, or prior-branch state — the baseline shape for a
// first-ever attempt at a ticket.
func freshTicketingTask() engine.Task {
	return engine.Task{
		ID:          "task-fresh-1",
		TicketID:    "TICKET-100",
		Title:       "Fix login bug",
		Description: "The login page returns a 500 error when the password is empty.",
		TicketURL:   "https://example.atlassian.net/browse/TICKET-100",
		RepoURL:     "https://github.com/org/repo",
		Labels:      []string{"bug", "backend"},
	}
}

// sessionResumeTask returns a task shaped like a retry that resumes a
// persisted Claude Code session via --resume.
func sessionResumeTask() engine.Task {
	task := freshTicketingTask()
	task.ID = "task-resume-1"
	task.TaskRunID = "tr-resume-1"
	task.SessionID = "9f2b7e2b-6c1b-4a34-9c9d-1a2b3c4d5e6f"
	return task
}

// priorMergeRequestTask returns a task shaped like a review follow-up or
// retry where a merge request already exists at PriorMergeRequestURL.
func priorMergeRequestTask() engine.Task {
	task := freshTicketingTask()
	task.ID = "task-priormr-1"
	task.PriorMergeRequestURL = "https://github.com/org/repo/pull/42"
	return task
}

// priorBranchTask returns a task shaped like a continuation where a
// previous, non-session-persisted attempt already pushed work to a branch.
func priorBranchTask() engine.Task {
	task := freshTicketingTask()
	task.ID = "task-priorbranch-1"
	task.PriorBranchName = "osmia/TICKET-100"
	return task
}

// incidentShapedTask returns a task shaped like an incident.io triage
// dispatch: no RepoURL, incident-derived title/description/ticket URL/labels.
func incidentShapedTask() engine.Task {
	return engine.Task{
		ID:          "task-incident-1",
		TicketID:    "01HZINCIDENTXYZ",
		Title:       "Database is down",
		Description: "Customers reporting 500s",
		TicketURL:   "https://app.incident.io/incidents/01HZINCIDENTXYZ",
		Labels:      []string{"osmia:source:incident-io"},
	}
}

// specSnapshot is a stable, JSON-serialisable projection of the parts of an
// ExecutionSpec these golden tests pin: command args, env names+values,
// volume mounts, and secret refs. ResourceRequests/Limits and
// ActiveDeadlineSeconds are pass-throughs of EngineConfig rather than
// derived from task shape, and are already covered by TestBuildExecutionSpec.
type specSnapshot struct {
	Command       []string                       `json:"command"`
	Env           map[string]string              `json:"env"`
	SecretKeyRefs map[string]engine.SecretKeyRef `json:"secret_key_refs"`
	Volumes       []engine.VolumeMount           `json:"volumes"`
}

func snapshotSpec(spec *engine.ExecutionSpec) specSnapshot {
	return specSnapshot{
		Command:       spec.Command,
		Env:           spec.Env,
		SecretKeyRefs: spec.SecretKeyRefs,
		Volumes:       spec.Volumes,
	}
}

// assertGolden marshals got to indented JSON and compares it against the
// named file under testdata/, regenerating the file when -update is passed.
func assertGolden(t *testing.T, name string, got any) {
	t.Helper()

	data, err := json.MarshalIndent(got, "", "  ")
	require.NoError(t, err)
	data = append(data, '\n')

	goldenPath := filepath.Join("testdata", name)
	if *updateGolden {
		require.NoError(t, os.WriteFile(goldenPath, data, 0o600))
	}

	want, err := os.ReadFile(goldenPath)
	require.NoError(t, err, "golden file %s missing — run with -update to generate it", name)
	assert.Equal(t, string(want), string(data),
		"%s drifted from its pinned golden; if this is a deliberate change, regenerate "+
			"with -update and review the diff carefully", name)
}

// TestGoldenBuildExecutionSpec pins BuildExecutionSpec's command args, env,
// volume mounts, and secret refs for five task shapes.
func TestGoldenBuildExecutionSpec(t *testing.T) {
	tests := []struct {
		name       string
		opts       []Option
		task       engine.Task
		config     engine.EngineConfig
		goldenName string
	}{
		{
			name:       "fresh ticketing task",
			task:       freshTicketingTask(),
			goldenName: "execspec_fresh_ticketing.golden",
		},
		{
			name:       "session resume",
			opts:       []Option{WithSessionStore(&stubSessionStore{})},
			task:       sessionResumeTask(),
			goldenName: "execspec_session_resume.golden",
		},
		{
			name:       "prior merge request",
			task:       priorMergeRequestTask(),
			goldenName: "execspec_prior_merge_request.golden",
		},
		{
			name:       "prior branch continuation",
			task:       priorBranchTask(),
			goldenName: "execspec_prior_branch.golden",
		},
		{
			name: "incident shaped",
			task: incidentShapedTask(),
			config: engine.EngineConfig{
				AppendSystemPrompt: "Post a summary to the incident Slack channel via MCP.",
			},
			goldenName: "execspec_incident_shaped.golden",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := New(tt.opts...)
			spec, err := e.BuildExecutionSpec(tt.task, tt.config)
			require.NoError(t, err)
			assertGolden(t, tt.goldenName, snapshotSpec(spec))
		})
	}
}

// TestGoldenBuildPrompt pins BuildPrompt's full output for four task shapes.
// The fifth shape (incident-shaped, empty RepoURL) is already pinned by
// TestBuildPrompt_IncidentTriageGolden (testdata/incident_triage_prompt.golden)
// and is not duplicated here.
func TestGoldenBuildPrompt(t *testing.T) {
	tests := []struct {
		name       string
		task       engine.Task
		goldenName string
	}{
		{"fresh ticketing task", freshTicketingTask(), "prompt_fresh_ticketing.golden"},
		{"session resume", sessionResumeTask(), "prompt_session_resume.golden"},
		{"prior merge request", priorMergeRequestTask(), "prompt_prior_merge_request.golden"},
		{"prior branch continuation", priorBranchTask(), "prompt_prior_branch.golden"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := New()
			prompt, err := e.BuildPrompt(tt.task)
			require.NoError(t, err)

			goldenPath := filepath.Join("testdata", tt.goldenName)
			if *updateGolden {
				require.NoError(t, os.WriteFile(goldenPath, []byte(prompt), 0o600))
			}

			want, err := os.ReadFile(goldenPath)
			require.NoError(t, err, "golden file %s missing — run with -update to generate it", tt.goldenName)
			assert.Equal(t, string(want), prompt,
				"%s drifted from its pinned golden; if this is a deliberate change, regenerate "+
					"with -update and review the diff carefully", tt.goldenName)
		})
	}
}
