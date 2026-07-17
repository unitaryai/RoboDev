package taskrun

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/unitaryai/osmia/pkg/engine"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestSQLiteStore(t *testing.T, path string) *SQLiteStore {
	t.Helper()
	store, err := NewSQLiteStore(context.Background(), path, testLogger())
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// fullyPopulatedTaskRun returns a TaskRun with every field set to a
// non-zero value, used to verify round-trip fidelity through content_json.
func fullyPopulatedTaskRun() *TaskRun {
	heartbeat := time.Date(2026, 1, 15, 9, 0, 0, 0, time.UTC)
	return &TaskRun{
		ID:             "tr-full",
		IdempotencyKey: "key-full",
		TicketID:       "TICKET-42",
		Engine:         "claude-code",
		CurrentEngine:  "codex",
		EngineAttempts: []string{"claude-code", "codex"},
		State:          StateRunning,
		JobName:        "job-tr-full",
		CreatedAt:      time.Date(2026, 1, 15, 8, 0, 0, 0, time.UTC),
		UpdatedAt:      time.Date(2026, 1, 15, 8, 30, 0, 0, time.UTC),
		Result: &engine.TaskResult{
			Success:         true,
			MergeRequestURL: "https://example.com/mr/1",
			BranchName:      "osmia/tr-full",
			Summary:         "did the thing",
			TokenUsage:      &engine.TokenUsage{InputTokens: 100, OutputTokens: 200},
			CostEstimateUSD: 1.23,
			ExitCode:        0,
		},
		HumanQuestion:             "should I proceed?",
		RetryCount:                2,
		MaxRetries:                3,
		HeartbeatAt:               &heartbeat,
		HeartbeatTTLSeconds:       300,
		TokensConsumed:            5000,
		FilesChanged:              7,
		ToolCallsTotal:            42,
		LastToolName:              "Edit",
		ConsecutiveIdenticalTools: 1,
		CostUSD:                   4.56,
		DiagnosisHistory: []DiagnosisRecord{
			{
				Mode:            "flaky-test",
				Confidence:      0.8,
				Evidence:        []string{"log line 1", "log line 2"},
				Prescription:    "retry",
				SuggestedEngine: "aider",
				DiagnosedAt:     time.Date(2026, 1, 15, 8, 15, 0, 0, time.UTC),
			},
		},
		TournamentID:          "tourn-1",
		CandidateIndex:        2,
		TournamentState:       "active",
		ApprovalGateType:      "pre_merge",
		SessionID:             "session-abc",
		ContinuationCount:     1,
		MaxContinuations:      3,
		ConfiguredMaxTurns:    50,
		NotificationThreadRef: "1700000000.000100",
		ParentTicketID:        "TICKET-1",
		ReviewCommentID:       "comment-1",
		ReviewThreadID:        "thread-1",
		ReviewPRURL:           "https://example.com/pr/1",
	}
}

func TestNewSQLiteStore_Construction(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{name: "temp file path", path: filepath.Join(t.TempDir(), "taskrun.db")},
		{name: "in-memory database", path: ":memory:"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			store, err := NewSQLiteStore(ctx, tt.path, testLogger())
			require.NoError(t, err)
			defer func() { _ = store.Close() }()

			list, err := store.List(ctx)
			require.NoError(t, err)
			assert.Empty(t, list)
		})
	}
}

func TestSQLiteStore_RoundTripFidelity(t *testing.T) {
	t.Parallel()

	store := newTestSQLiteStore(t, filepath.Join(t.TempDir(), "taskrun.db"))
	ctx := context.Background()

	want := fullyPopulatedTaskRun()
	require.NoError(t, store.Save(ctx, want))

	got, err := store.Get(ctx, want.ID)
	require.NoError(t, err)

	wantJSON, err := json.Marshal(want)
	require.NoError(t, err)
	gotJSON, err := json.Marshal(got)
	require.NoError(t, err)

	assert.JSONEq(t, string(wantJSON), string(gotJSON))
}

func TestSQLiteStore_SaveNilReturnsError(t *testing.T) {
	t.Parallel()

	store := newTestSQLiteStore(t, ":memory:")
	err := store.Save(context.Background(), nil)
	require.Error(t, err)
}

func TestSQLiteStore_UpsertSemantics(t *testing.T) {
	t.Parallel()

	store := newTestSQLiteStore(t, filepath.Join(t.TempDir(), "taskrun.db"))
	ctx := context.Background()

	tr := New("tr-1", "key-1", "TICKET-1", "claude-code")
	require.NoError(t, store.Save(ctx, tr))

	tr.State = StateRunning
	tr.JobName = "job-tr-1"
	tr.TokensConsumed = 1000
	require.NoError(t, store.Save(ctx, tr))

	got, err := store.Get(ctx, "tr-1")
	require.NoError(t, err)
	assert.Equal(t, StateRunning, got.State)
	assert.Equal(t, "job-tr-1", got.JobName)
	assert.Equal(t, 1000, got.TokensConsumed)

	list, err := store.List(ctx)
	require.NoError(t, err)
	assert.Len(t, list, 1, "upsert must not create a duplicate row")
}

func TestSQLiteStore_GetNotFound(t *testing.T) {
	t.Parallel()

	store := newTestSQLiteStore(t, ":memory:")
	ctx := context.Background()

	_, err := store.Get(ctx, "nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
	assert.ErrorIs(t, err, sql.ErrNoRows)
}

func TestSQLiteStore_List(t *testing.T) {
	tests := []struct {
		name  string
		setup []*TaskRun
		want  int
	}{
		{
			name:  "empty store returns empty list",
			setup: nil,
			want:  0,
		},
		{
			name: "returns all stored task runs",
			setup: []*TaskRun{
				New("tr-1", "key-1", "TICKET-1", "claude-code"),
				New("tr-2", "key-2", "TICKET-2", "codex"),
				New("tr-3", "key-3", "TICKET-3", "aider"),
			},
			want: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newTestSQLiteStore(t, ":memory:")
			ctx := context.Background()

			for _, tr := range tt.setup {
				require.NoError(t, store.Save(ctx, tr))
			}

			got, err := store.List(ctx)
			require.NoError(t, err)
			assert.Len(t, got, tt.want)
		})
	}
}

func TestSQLiteStore_ListByTicketID(t *testing.T) {
	tests := []struct {
		name     string
		setup    []*TaskRun
		ticketID string
		want     int
	}{
		{
			name: "filters by ticket ID",
			setup: []*TaskRun{
				New("tr-1", "key-1", "TICKET-1", "claude-code"),
				New("tr-2", "key-2", "TICKET-1", "codex"),
				New("tr-3", "key-3", "TICKET-2", "aider"),
			},
			ticketID: "TICKET-1",
			want:     2,
		},
		{
			name: "returns empty for unmatched ticket ID",
			setup: []*TaskRun{
				New("tr-1", "key-1", "TICKET-1", "claude-code"),
			},
			ticketID: "TICKET-999",
			want:     0,
		},
		{
			name:     "empty store returns empty list",
			setup:    nil,
			ticketID: "TICKET-1",
			want:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newTestSQLiteStore(t, ":memory:")
			ctx := context.Background()

			for _, tr := range tt.setup {
				require.NoError(t, store.Save(ctx, tr))
			}

			got, err := store.ListByTicketID(ctx, tt.ticketID)
			require.NoError(t, err)
			assert.Len(t, got, tt.want)

			for _, tr := range got {
				assert.Equal(t, tt.ticketID, tr.TicketID)
			}
		})
	}
}

func TestSQLiteStore_ConcurrentSaves(t *testing.T) {
	t.Parallel()

	store := newTestSQLiteStore(t, filepath.Join(t.TempDir(), "taskrun.db"))
	ctx := context.Background()

	const n = 20
	var wg sync.WaitGroup
	errs := make(chan error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tr := New(fmt.Sprintf("tr-%d", i), fmt.Sprintf("key-%d", i), "TICKET-1", "claude-code")
			errs <- store.Save(ctx, tr)
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		assert.NoError(t, err)
	}

	list, err := store.List(ctx)
	require.NoError(t, err)
	assert.Len(t, list, n)
}

func TestSQLiteStore_Persistence(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "taskrun.db")
	ctx := context.Background()

	store1, err := NewSQLiteStore(ctx, dbPath, testLogger())
	require.NoError(t, err)

	tr := New("tr-persist", "key-persist", "TICKET-1", "claude-code")
	require.NoError(t, store1.Save(ctx, tr))
	require.NoError(t, store1.Close())

	store2 := newTestSQLiteStore(t, dbPath)
	got, err := store2.Get(ctx, "tr-persist")
	require.NoError(t, err)
	assert.Equal(t, "tr-persist", got.ID)
}
