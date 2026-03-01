package watchdog

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryProfileStore(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryProfileStore()

	key := ProfileKey{RepoPattern: "repo-a", Engine: "claude-code", TaskType: "fix"}
	profile := &CalibratedProfile{
		Key: key,
		Thresholds: map[Signal]*Percentiles{
			SignalTokenRate: {P50: 500, P90: 900, P99: 990, SampleCount: 20},
		},
		LastUpdated: time.Now(),
		SampleCount: 20,
	}

	t.Run("get returns nil for missing key", func(t *testing.T) {
		assert.Nil(t, store.Get(ctx, key))
	})

	t.Run("put and get round-trip", func(t *testing.T) {
		store.Put(ctx, profile)
		got := store.Get(ctx, key)
		require.NotNil(t, got)
		assert.Equal(t, key, got.Key)
		assert.Equal(t, 20, got.SampleCount)
	})

	t.Run("list returns all profiles", func(t *testing.T) {
		profiles := store.List(ctx)
		assert.Len(t, profiles, 1)
	})

	t.Run("put nil is a no-op", func(t *testing.T) {
		store.Put(ctx, nil)
		profiles := store.List(ctx)
		assert.Len(t, profiles, 1)
	})
}

func TestProfileResolver_ExactMatch(t *testing.T) {
	ctx := context.Background()
	cal := NewCalibrator(calibratorLogger())
	store := NewMemoryProfileStore()

	key := ProfileKey{RepoPattern: "repo-a", Engine: "claude-code", TaskType: "fix"}
	store.Put(ctx, &CalibratedProfile{
		Key:         key,
		Thresholds:  map[Signal]*Percentiles{},
		LastUpdated: time.Now(),
		SampleCount: 15,
	})

	resolver := NewProfileResolver(store, cal, 10)
	profile := resolver.ResolveProfile(ctx, "repo-a", "claude-code", "fix")
	require.NotNil(t, profile)
	assert.Equal(t, key, profile.Key)
}

func TestProfileResolver_PartialMatch(t *testing.T) {
	ctx := context.Background()
	cal := NewCalibrator(calibratorLogger())
	store := NewMemoryProfileStore()

	// Only a wildcard repo profile exists.
	wildcardKey := ProfileKey{RepoPattern: "*", Engine: "claude-code", TaskType: "fix"}
	store.Put(ctx, &CalibratedProfile{
		Key:         wildcardKey,
		Thresholds:  map[Signal]*Percentiles{},
		LastUpdated: time.Now(),
		SampleCount: 12,
	})

	resolver := NewProfileResolver(store, cal, 10)
	profile := resolver.ResolveProfile(ctx, "repo-b", "claude-code", "fix")
	require.NotNil(t, profile)
	assert.Equal(t, wildcardKey, profile.Key)
}

func TestProfileResolver_GlobalFallback(t *testing.T) {
	ctx := context.Background()
	cal := NewCalibrator(calibratorLogger())
	store := NewMemoryProfileStore()

	globalKey := ProfileKey{RepoPattern: "*", Engine: "*", TaskType: "feature"}
	store.Put(ctx, &CalibratedProfile{
		Key:         globalKey,
		Thresholds:  map[Signal]*Percentiles{},
		LastUpdated: time.Now(),
		SampleCount: 20,
	})

	resolver := NewProfileResolver(store, cal, 10)
	profile := resolver.ResolveProfile(ctx, "repo-x", "codex", "feature")
	require.NotNil(t, profile)
	assert.Equal(t, globalKey, profile.Key)
}

func TestProfileResolver_ColdStart(t *testing.T) {
	ctx := context.Background()
	cal := NewCalibrator(calibratorLogger())
	store := NewMemoryProfileStore()

	// Profile exists but with insufficient samples.
	key := ProfileKey{RepoPattern: "repo-a", Engine: "claude-code", TaskType: "fix"}
	store.Put(ctx, &CalibratedProfile{
		Key:         key,
		Thresholds:  map[Signal]*Percentiles{},
		LastUpdated: time.Now(),
		SampleCount: 5, // below min of 10
	})

	resolver := NewProfileResolver(store, cal, 10)
	profile := resolver.ResolveProfile(ctx, "repo-a", "claude-code", "fix")
	assert.Nil(t, profile, "should return nil when insufficient samples")
}

func TestProfileResolver_NoData(t *testing.T) {
	ctx := context.Background()
	cal := NewCalibrator(calibratorLogger())
	store := NewMemoryProfileStore()

	resolver := NewProfileResolver(store, cal, 10)
	profile := resolver.ResolveProfile(ctx, "repo-x", "codex", "feature")
	assert.Nil(t, profile)
}

func TestProfileResolver_RefreshProfile(t *testing.T) {
	ctx := context.Background()
	cal := NewCalibrator(calibratorLogger())
	store := NewMemoryProfileStore()

	// Record enough observations.
	for i := 0; i < 15; i++ {
		cal.Record(ctx, Observation{
			RepoURL:              "repo-a",
			Engine:               "claude-code",
			TaskType:             "fix",
			TokensConsumed:       int64((i + 1) * 1000),
			ToolCallsTotal:       (i + 1) * 5,
			FilesChanged:         i + 1,
			CostEstimateUSD:      float64(i+1) * 0.10,
			DurationSeconds:      300,
			ConsecutiveIdentical: i + 1,
			CompletedAt:          time.Now(),
		})
	}

	resolver := NewProfileResolver(store, cal, 10)
	key := ProfileKey{RepoPattern: "repo-a", Engine: "claude-code", TaskType: "fix"}
	profile := resolver.RefreshProfile(ctx, key)

	require.NotNil(t, profile)
	assert.Equal(t, 15, profile.SampleCount)
	assert.NotEmpty(t, profile.Thresholds)

	// Verify it was stored.
	stored := store.Get(ctx, key)
	require.NotNil(t, stored)
	assert.Equal(t, 15, stored.SampleCount)
}

func TestProfileResolver_MinSamplesDefault(t *testing.T) {
	cal := NewCalibrator(calibratorLogger())
	store := NewMemoryProfileStore()

	// Pass 0 to test default behaviour.
	resolver := NewProfileResolver(store, cal, 0)
	assert.NotNil(t, resolver)
}

func TestMatchRepoGlob(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		value   string
		want    bool
	}{
		{
			name:    "wildcard matches everything",
			pattern: "*",
			value:   "https://github.com/org/repo",
			want:    true,
		},
		{
			name:    "exact match",
			pattern: "repo-a",
			value:   "repo-a",
			want:    true,
		},
		{
			name:    "no match",
			pattern: "repo-a",
			value:   "repo-b",
			want:    false,
		},
		{
			name:    "glob pattern",
			pattern: "repo-*",
			value:   "repo-abc",
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, matchRepoGlob(tt.pattern, tt.value))
		})
	}
}

func TestPickPercentile(t *testing.T) {
	p := &Percentiles{P50: 50, P90: 90, P99: 99}

	tests := []struct {
		threshold string
		expect    float64
	}{
		{"p50", 50},
		{"p90", 90},
		{"p99", 99},
		{"unknown", 90}, // defaults to p90
	}

	for _, tt := range tests {
		t.Run(tt.threshold, func(t *testing.T) {
			assert.Equal(t, tt.expect, pickPercentile(p, tt.threshold))
		})
	}
}
