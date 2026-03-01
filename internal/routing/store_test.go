package routing

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryFingerprintStore_SaveAndGet(t *testing.T) {
	store := NewMemoryFingerprintStore()
	ctx := context.Background()

	fp := NewEngineFingerprint("claude-code")
	fp.Update(TaskOutcome{
		EngineName: "claude-code",
		TaskType:   "bug_fix",
		Success:    true,
	})

	err := store.Save(ctx, fp)
	require.NoError(t, err)

	got, err := store.Get(ctx, "claude-code")
	require.NoError(t, err)
	assert.Equal(t, "claude-code", got.EngineName)
	assert.Equal(t, 1, got.TotalTasks)
}

func TestMemoryFingerprintStore_GetNotFound(t *testing.T) {
	store := NewMemoryFingerprintStore()
	ctx := context.Background()

	_, err := store.Get(ctx, "nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestMemoryFingerprintStore_SaveNil(t *testing.T) {
	store := NewMemoryFingerprintStore()
	ctx := context.Background()

	err := store.Save(ctx, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

func TestMemoryFingerprintStore_List(t *testing.T) {
	store := NewMemoryFingerprintStore()
	ctx := context.Background()

	// Empty store returns empty list.
	list, err := store.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, list)

	// Add two fingerprints.
	require.NoError(t, store.Save(ctx, NewEngineFingerprint("engine-a")))
	require.NoError(t, store.Save(ctx, NewEngineFingerprint("engine-b")))

	list, err = store.List(ctx)
	require.NoError(t, err)
	assert.Len(t, list, 2)

	names := make(map[string]bool)
	for _, fp := range list {
		names[fp.EngineName] = true
	}
	assert.True(t, names["engine-a"])
	assert.True(t, names["engine-b"])
}

func TestMemoryFingerprintStore_SaveOverwrites(t *testing.T) {
	store := NewMemoryFingerprintStore()
	ctx := context.Background()

	fp1 := NewEngineFingerprint("claude-code")
	fp1.Update(TaskOutcome{EngineName: "claude-code", TaskType: "bug_fix", Success: true})
	require.NoError(t, store.Save(ctx, fp1))

	fp2 := NewEngineFingerprint("claude-code")
	fp2.Update(TaskOutcome{EngineName: "claude-code", TaskType: "refactor", Success: false})
	fp2.Update(TaskOutcome{EngineName: "claude-code", TaskType: "refactor", Success: false})
	require.NoError(t, store.Save(ctx, fp2))

	got, err := store.Get(ctx, "claude-code")
	require.NoError(t, err)
	assert.Equal(t, 2, got.TotalTasks, "overwritten fingerprint should have latest data")
}
