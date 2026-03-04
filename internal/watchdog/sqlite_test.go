package watchdog

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestProfileStore(t *testing.T, path string) *SQLiteProfileStore {
	t.Helper()
	store, err := NewSQLiteProfileStore(path, testLogger())
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	return store
}

func TestSQLiteProfileStore_PutAndGet(t *testing.T) {
	t.Parallel()

	store := newTestProfileStore(t, filepath.Join(t.TempDir(), "prof.db"))
	ctx := context.Background()

	key := ProfileKey{RepoPattern: "github.com/org/repo", Engine: "claude-code", TaskType: "bug_fix"}
	profile := &CalibratedProfile{
		Key:         key,
		Thresholds:  nil,
		LastUpdated: time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
		SampleCount: 25,
	}

	store.Put(ctx, profile)

	got := store.Get(ctx, key)
	require.NotNil(t, got)
	assert.Equal(t, key, got.Key)
	assert.Equal(t, 25, got.SampleCount)
}

func TestSQLiteProfileStore_UpdateNoDuplicate(t *testing.T) {
	t.Parallel()

	store := newTestProfileStore(t, filepath.Join(t.TempDir(), "prof.db"))
	ctx := context.Background()

	key := ProfileKey{RepoPattern: "*", Engine: "aider", TaskType: "feature"}
	profile := &CalibratedProfile{
		Key:         key,
		LastUpdated: time.Now(),
		SampleCount: 10,
	}

	store.Put(ctx, profile)

	profile.SampleCount = 20
	store.Put(ctx, profile)

	list := store.List(ctx)
	assert.Len(t, list, 1)
	assert.Equal(t, 20, list[0].SampleCount)
}

func TestSQLiteProfileStore_List(t *testing.T) {
	t.Parallel()

	store := newTestProfileStore(t, filepath.Join(t.TempDir(), "prof.db"))
	ctx := context.Background()

	store.Put(ctx, &CalibratedProfile{
		Key:         ProfileKey{RepoPattern: "repo-a", Engine: "e1", TaskType: "t1"},
		LastUpdated: time.Now(),
		SampleCount: 5,
	})
	store.Put(ctx, &CalibratedProfile{
		Key:         ProfileKey{RepoPattern: "repo-b", Engine: "e2", TaskType: "t2"},
		LastUpdated: time.Now(),
		SampleCount: 8,
	})

	list := store.List(ctx)
	assert.Len(t, list, 2)
}

func TestSQLiteProfileStore_Persistence(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "prof.db")
	ctx := context.Background()

	key := ProfileKey{RepoPattern: "org/*", Engine: "codex", TaskType: "refactor"}

	store1, err := NewSQLiteProfileStore(dbPath, testLogger())
	require.NoError(t, err)
	store1.Put(ctx, &CalibratedProfile{
		Key:         key,
		LastUpdated: time.Now(),
		SampleCount: 15,
	})
	require.NoError(t, store1.Close())

	store2 := newTestProfileStore(t, dbPath)
	got := store2.Get(ctx, key)
	require.NotNil(t, got)
	assert.Equal(t, 15, got.SampleCount)
}
