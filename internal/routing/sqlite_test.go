package routing

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestFingerprintStore(t *testing.T, path string) *SQLiteFingerprintStore {
	t.Helper()
	store, err := NewSQLiteFingerprintStore(path, testLogger())
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	return store
}

func TestSQLiteFingerprintStore_SaveAndGet(t *testing.T) {
	t.Parallel()

	store := newTestFingerprintStore(t, filepath.Join(t.TempDir(), "fp.db"))
	ctx := context.Background()

	fp := NewEngineFingerprint("claude-code")
	fp.TotalTasks = 42
	fp.LastUpdated = time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)

	err := store.Save(ctx, fp)
	require.NoError(t, err)

	got, err := store.Get(ctx, "claude-code")
	require.NoError(t, err)
	assert.Equal(t, "claude-code", got.EngineName)
	assert.Equal(t, 42, got.TotalTasks)
}

func TestSQLiteFingerprintStore_UpdateNoDuplicate(t *testing.T) {
	t.Parallel()

	store := newTestFingerprintStore(t, filepath.Join(t.TempDir(), "fp.db"))
	ctx := context.Background()

	fp := NewEngineFingerprint("aider")
	fp.TotalTasks = 10

	require.NoError(t, store.Save(ctx, fp))

	fp.TotalTasks = 20
	require.NoError(t, store.Save(ctx, fp))

	list, err := store.List(ctx)
	require.NoError(t, err)
	assert.Len(t, list, 1)
	assert.Equal(t, 20, list[0].TotalTasks)
}

func TestSQLiteFingerprintStore_List(t *testing.T) {
	t.Parallel()

	store := newTestFingerprintStore(t, filepath.Join(t.TempDir(), "fp.db"))
	ctx := context.Background()

	require.NoError(t, store.Save(ctx, NewEngineFingerprint("engine-a")))
	require.NoError(t, store.Save(ctx, NewEngineFingerprint("engine-b")))

	list, err := store.List(ctx)
	require.NoError(t, err)
	assert.Len(t, list, 2)
}

func TestSQLiteFingerprintStore_Persistence(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "fp.db")
	ctx := context.Background()

	// Open, save, and close.
	store1, err := NewSQLiteFingerprintStore(dbPath, testLogger())
	require.NoError(t, err)

	fp := NewEngineFingerprint("codex")
	fp.TotalTasks = 7
	require.NoError(t, store1.Save(ctx, fp))
	require.NoError(t, store1.Close())

	// Reopen and verify the data survived.
	store2 := newTestFingerprintStore(t, dbPath)
	got, err := store2.Get(ctx, "codex")
	require.NoError(t, err)
	assert.Equal(t, "codex", got.EngineName)
	assert.Equal(t, 7, got.TotalTasks)
}
