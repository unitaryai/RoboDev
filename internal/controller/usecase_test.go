package controller

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/unitaryai/osmia/internal/config"
	"github.com/unitaryai/osmia/internal/taskrun"
	"github.com/unitaryai/osmia/internal/usecase"
)

func TestUseCaseFor(t *testing.T) {
	tests := []struct {
		name string
		tr   *taskrun.TaskRun
		want string
	}{
		{
			name: "returns the persisted use case when set",
			tr:   &taskrun.TaskRun{ID: "tr-incident-01hz-created-1", UseCase: usecase.NameTicketing},
			want: usecase.NameTicketing,
		},
		{
			name: "infers incident triage from the ID prefix when unset",
			tr:   &taskrun.TaskRun{ID: "tr-incident-01hz-created-1"},
			want: usecase.NameIncidentTriage,
		},
		{
			name: "infers ticketing for any other ID shape when unset",
			tr:   &taskrun.TaskRun{ID: "tr-TICKET-1-1700000000000"},
			want: usecase.NameTicketing,
		},
		{
			name: "infers ticketing for an empty ID when unset",
			tr:   &taskrun.TaskRun{},
			want: usecase.NameTicketing,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, useCaseFor(tt.tr))
		})
	}
}

func TestNewUseCaseRegistry(t *testing.T) {
	reg := newUseCaseRegistry()

	ticketing, ok := reg.Get(usecase.NameTicketing)
	assert.True(t, ok)
	assert.Equal(t, usecase.ModeClonePushMR, ticketing.ExecutionMode)
	assert.Equal(t, usecase.Gates{
		ApprovalGates:   true,
		CostEstimation:  true,
		EngineSelector:  true,
		GuardRails:      true,
		MarkInProgress:  true,
		MemoryQuery:     true,
		NotifyStart:     true,
		RepoURLRequired: true,
		Tournament:      true,
	}, ticketing.Gates)

	incident, ok := reg.Get(usecase.NameIncidentTriage)
	assert.True(t, ok)
	assert.Equal(t, usecase.ModeAPIRead, incident.ExecutionMode)
	assert.Equal(t, usecase.Gates{}, incident.Gates)
}

// TestNewLaunchTaskRunSetsTenant pins that a TaskRun is tenanted at
// creation, for both flows. The tenant is what keeps one flow's extracted
// facts out of the other's prompt, and it is stamped here rather than at
// launch so a TaskRun held at a pre-launch gate still carries it.
func TestNewLaunchTaskRunSetsTenant(t *testing.T) {
	tests := []struct {
		name       string
		useCase    string
		wantTenant string
	}{
		{
			name:       "ticketing",
			useCase:    usecase.NameTicketing,
			wantTenant: taskrun.TenantTicketing,
		},
		{
			name:       "incident triage",
			useCase:    usecase.NameIncidentTriage,
			wantTenant: taskrun.TenantIncidentTriage,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewReconciler(&config.Config{}, testLogger())

			tr := r.newLaunchTaskRun(context.Background(), "tr-1", "key-1", "TICKET-1", "claude-code", tt.useCase)

			assert.Equal(t, tt.useCase, tr.UseCase)
			assert.Equal(t, tt.wantTenant, tr.TenantID)
		})
	}
}

// TestTenantConstantsMatchUseCaseNames pins the one-for-one mapping the
// assignment in newLaunchTaskRun relies on. The two sets of constants live in
// different packages on purpose, so nothing else would catch them drifting
// apart and silently splitting a flow's memory in two.
func TestTenantConstantsMatchUseCaseNames(t *testing.T) {
	assert.Equal(t, usecase.NameTicketing, taskrun.TenantTicketing)
	assert.Equal(t, usecase.NameIncidentTriage, taskrun.TenantIncidentTriage)
}

// TestTagTaskRun covers the helper that keeps UseCase and TenantID in step.
func TestTagTaskRun(t *testing.T) {
	tr := taskrun.New("tr-1", "key-1", "TICKET-1", "claude-code")

	tagTaskRun(tr, usecase.NameTicketing)

	assert.Equal(t, usecase.NameTicketing, tr.UseCase)
	assert.Equal(t, taskrun.TenantTicketing, tr.TenantID)
}

// TestEveryTaskRunConstructorIsTagged is a source-level guard rather than a
// behavioural test, because the alternative is standing up the tournament,
// judge, follow-up and repo-URL-poll paths in full just to read one field.
//
// It exists because the failure it catches is silent: a launch path that
// builds a TaskRun with taskrun.New and forgets to tag it writes its
// extracted facts under no tenant, where no tenanted query will return them.
// Nothing errors, and the knowledge is just quietly lost. That is exactly
// what happened to all four of these paths when TenantID was introduced.
//
// Any new taskrun.New call site must either go through newLaunchTaskRun or
// call tagTaskRun.
func TestEveryTaskRunConstructorIsTagged(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)

	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}

		src, err := os.ReadFile(file)
		require.NoError(t, err)
		lines := strings.Split(string(src), "\n")

		for i, line := range lines {
			if !strings.Contains(line, "taskrun.New(") {
				continue
			}
			// newLaunchTaskRun is the shared path and tags via tagTaskRun.
			if file == "launch.go" {
				continue
			}

			// Look ahead a short window for the tag call. Every current site
			// tags within a few lines of construction.
			window := lines[i:min(i+14, len(lines))]
			assert.Truef(t, strings.Contains(strings.Join(window, "\n"), "tagTaskRun("),
				"%s:%d constructs a TaskRun without calling tagTaskRun nearby; "+
					"its extracted memory would be written untenanted", file, i+1)
		}
	}
}
