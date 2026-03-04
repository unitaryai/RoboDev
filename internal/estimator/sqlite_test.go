package estimator

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestEstimatorStore(t *testing.T, path string) *SQLiteEstimatorStore {
	t.Helper()
	store, err := NewSQLiteEstimatorStore(path, testLogger())
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	return store
}

func TestSQLiteEstimatorStore_SaveAndQuerySimilar(t *testing.T) {
	t.Parallel()

	store := newTestEstimatorStore(t, filepath.Join(t.TempDir(), "est.db"))
	ctx := context.Background()

	outcome := PredictionOutcome{
		TaskRunID: "tr-1",
		Engine:    "claude-code",
		ComplexityScore: ComplexityScore{
			Overall:    0.5,
			Dimensions: map[string]float64{"repo_size": 0.4, "description_complexity": 0.6},
		},
		ActualCost:     1.25,
		ActualDuration: 30 * time.Second,
		Success:        true,
		RecordedAt:     time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
	}

	require.NoError(t, store.SaveOutcome(ctx, outcome))

	query := ComplexityScore{
		Overall:    0.5,
		Dimensions: map[string]float64{"repo_size": 0.4, "description_complexity": 0.6},
	}
	results, err := store.QuerySimilar(ctx, query, "", 5)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "tr-1", results[0].TaskRunID)
}

func TestSQLiteEstimatorStore_UpdateNoDuplicate(t *testing.T) {
	t.Parallel()

	store := newTestEstimatorStore(t, filepath.Join(t.TempDir(), "est.db"))
	ctx := context.Background()

	outcome := PredictionOutcome{
		TaskRunID: "tr-dup",
		Engine:    "aider",
		ComplexityScore: ComplexityScore{
			Overall:    0.3,
			Dimensions: map[string]float64{"repo_size": 0.3},
		},
		ActualCost: 0.50,
		Success:    true,
		RecordedAt: time.Now(),
	}

	require.NoError(t, store.SaveOutcome(ctx, outcome))

	outcome.ActualCost = 0.75
	require.NoError(t, store.SaveOutcome(ctx, outcome))

	query := ComplexityScore{
		Overall:    0.3,
		Dimensions: map[string]float64{"repo_size": 0.3},
	}
	results, err := store.QuerySimilar(ctx, query, "", 10)
	require.NoError(t, err)
	assert.Len(t, results, 1)
}

func TestSQLiteEstimatorStore_QueryByEngine(t *testing.T) {
	t.Parallel()

	store := newTestEstimatorStore(t, filepath.Join(t.TempDir(), "est.db"))
	ctx := context.Background()

	score := ComplexityScore{
		Overall:    0.5,
		Dimensions: map[string]float64{"repo_size": 0.5},
	}

	require.NoError(t, store.SaveOutcome(ctx, PredictionOutcome{
		TaskRunID:       "tr-a",
		Engine:          "claude-code",
		ComplexityScore: score,
		Success:         true,
		RecordedAt:      time.Now(),
	}))
	require.NoError(t, store.SaveOutcome(ctx, PredictionOutcome{
		TaskRunID:       "tr-b",
		Engine:          "aider",
		ComplexityScore: score,
		Success:         true,
		RecordedAt:      time.Now(),
	}))

	results, err := store.QuerySimilar(ctx, score, "claude-code", 10)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "claude-code", results[0].Engine)
}

func TestSQLiteEstimatorStore_Persistence(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "est.db")
	ctx := context.Background()

	store1, err := NewSQLiteEstimatorStore(dbPath, testLogger())
	require.NoError(t, err)

	outcome := PredictionOutcome{
		TaskRunID: "tr-persist",
		Engine:    "codex",
		ComplexityScore: ComplexityScore{
			Overall:    0.7,
			Dimensions: map[string]float64{"repo_size": 0.7},
		},
		ActualCost: 2.0,
		Success:    true,
		RecordedAt: time.Now(),
	}
	require.NoError(t, store1.SaveOutcome(ctx, outcome))
	require.NoError(t, store1.Close())

	store2 := newTestEstimatorStore(t, dbPath)
	results, err := store2.QuerySimilar(ctx, outcome.ComplexityScore, "", 5)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "tr-persist", results[0].TaskRunID)
}
