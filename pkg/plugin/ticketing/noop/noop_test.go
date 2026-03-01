package noop

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))
}

func TestPollReadyTickets_NoTaskFile(t *testing.T) {
	b := New()
	tickets, err := b.PollReadyTickets(context.Background())
	require.NoError(t, err)
	assert.Empty(t, tickets)
}

func TestPollReadyTickets_ValidTaskFile(t *testing.T) {
	content := `- id: "task-1"
  title: "Fix login bug"
  description: "Users cannot log in with SSO"
  repo_url: "https://github.com/example/app"
  labels:
    - bug
    - priority-high
- id: "task-2"
  title: "Add dark mode"
  repo_url: "https://github.com/example/app"
`

	taskFile := filepath.Join(t.TempDir(), "tasks.yaml")
	require.NoError(t, os.WriteFile(taskFile, []byte(content), 0o644))

	b := NewWithTaskFile(testLogger(), taskFile)
	tickets, err := b.PollReadyTickets(context.Background())
	require.NoError(t, err)
	require.Len(t, tickets, 2)

	assert.Equal(t, "task-1", tickets[0].ID)
	assert.Equal(t, "Fix login bug", tickets[0].Title)
	assert.Equal(t, "Users cannot log in with SSO", tickets[0].Description)
	assert.Equal(t, "https://github.com/example/app", tickets[0].RepoURL)
	assert.Equal(t, []string{"bug", "priority-high"}, tickets[0].Labels)

	assert.Equal(t, "task-2", tickets[1].ID)
	assert.Equal(t, "Add dark mode", tickets[1].Title)
}

func TestPollReadyTickets_EmptyFile(t *testing.T) {
	taskFile := filepath.Join(t.TempDir(), "tasks.yaml")
	require.NoError(t, os.WriteFile(taskFile, []byte(""), 0o644))

	b := NewWithTaskFile(testLogger(), taskFile)
	tickets, err := b.PollReadyTickets(context.Background())
	require.NoError(t, err)
	assert.Empty(t, tickets)
}

func TestPollReadyTickets_MalformedYAML(t *testing.T) {
	content := `this is not valid yaml: [[[`

	taskFile := filepath.Join(t.TempDir(), "tasks.yaml")
	require.NoError(t, os.WriteFile(taskFile, []byte(content), 0o644))

	b := NewWithTaskFile(testLogger(), taskFile)
	tickets, err := b.PollReadyTickets(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing task file")
	assert.Nil(t, tickets)
}

func TestPollReadyTickets_MissingFile(t *testing.T) {
	b := NewWithTaskFile(testLogger(), "/nonexistent/tasks.yaml")
	tickets, err := b.PollReadyTickets(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading file")
	assert.Nil(t, tickets)
}

func TestPollReadyTickets_ExcludesProcessed(t *testing.T) {
	content := `- id: "task-1"
  title: "First task"
- id: "task-2"
  title: "Second task"
`

	taskFile := filepath.Join(t.TempDir(), "tasks.yaml")
	require.NoError(t, os.WriteFile(taskFile, []byte(content), 0o644))

	b := NewWithTaskFile(testLogger(), taskFile)

	// Mark task-1 as in-progress.
	require.NoError(t, b.MarkInProgress(context.Background(), "task-1"))

	// Only task-2 should be returned.
	tickets, err := b.PollReadyTickets(context.Background())
	require.NoError(t, err)
	require.Len(t, tickets, 1)
	assert.Equal(t, "task-2", tickets[0].ID)
}

func TestName(t *testing.T) {
	b := New()
	assert.Equal(t, "noop", b.Name())
}

func TestInterfaceVersion(t *testing.T) {
	b := New()
	assert.Equal(t, 1, b.InterfaceVersion())
}
