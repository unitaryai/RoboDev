package watchdog

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func calibratorLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestPercentile(t *testing.T) {
	tests := []struct {
		name   string
		data   []float64
		p      float64
		expect float64
	}{
		{
			name:   "single element p50",
			data:   []float64{42},
			p:      0.50,
			expect: 42,
		},
		{
			name:   "two elements p50",
			data:   []float64{10, 20},
			p:      0.50,
			expect: 15,
		},
		{
			name:   "ten elements p90",
			data:   []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
			p:      0.90,
			expect: 9.1,
		},
		{
			name:   "ten elements p50",
			data:   []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
			p:      0.50,
			expect: 5.5,
		},
		{
			name:   "ten elements p99",
			data:   []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
			p:      0.99,
			expect: 9.91,
		},
		{
			name:   "empty returns zero",
			data:   []float64{},
			p:      0.50,
			expect: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := percentile(tt.data, tt.p)
			assert.InDelta(t, tt.expect, got, 0.01)
		})
	}
}

func TestInsertSorted(t *testing.T) {
	tests := []struct {
		name   string
		sorted []float64
		val    float64
		expect []float64
	}{
		{
			name:   "insert into empty",
			sorted: nil,
			val:    5,
			expect: []float64{5},
		},
		{
			name:   "insert at beginning",
			sorted: []float64{3, 5, 7},
			val:    1,
			expect: []float64{1, 3, 5, 7},
		},
		{
			name:   "insert at end",
			sorted: []float64{1, 3, 5},
			val:    7,
			expect: []float64{1, 3, 5, 7},
		},
		{
			name:   "insert in middle",
			sorted: []float64{1, 3, 7},
			val:    5,
			expect: []float64{1, 3, 5, 7},
		},
		{
			name:   "insert duplicate",
			sorted: []float64{1, 3, 5},
			val:    3,
			expect: []float64{1, 3, 3, 5},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := insertSorted(tt.sorted, tt.val)
			assert.Equal(t, tt.expect, got)
		})
	}
}

func TestCalibrator_RecordAndGetPercentiles(t *testing.T) {
	cal := NewCalibrator(calibratorLogger())
	ctx := context.Background()

	// Record 20 observations with increasing token consumption.
	for i := 1; i <= 20; i++ {
		cal.Record(ctx, Observation{
			RepoURL:              "https://github.com/org/repo",
			Engine:               "claude-code",
			TaskType:             "bug_fix",
			TokensConsumed:       int64(i * 1000),
			ToolCallsTotal:       i * 5,
			FilesChanged:         i,
			CostEstimateUSD:      float64(i) * 0.10,
			DurationSeconds:      float64(i) * 60,
			ConsecutiveIdentical: i,
			CompletedAt:          time.Now(),
		})
	}

	key := ProfileKey{
		RepoPattern: "https://github.com/org/repo",
		Engine:      "claude-code",
		TaskType:    "bug_fix",
	}

	t.Run("sample count matches", func(t *testing.T) {
		assert.Equal(t, 20, cal.SampleCount(key))
	})

	t.Run("percentiles computed for token rate", func(t *testing.T) {
		p := cal.GetPercentiles(ctx, key, SignalTokenRate)
		require.NotNil(t, p)
		assert.Equal(t, 20, p.SampleCount)
		// Token rate = tokens / duration_min. Since each observation has
		// tokens=i*1000, duration=i*60s=i min, rate should be ~1000 for all.
		assert.InDelta(t, 1000, p.P50, 1)
		assert.InDelta(t, 1000, p.P90, 1)
	})

	t.Run("percentiles computed for consecutive identical calls", func(t *testing.T) {
		p := cal.GetPercentiles(ctx, key, SignalConsecutiveIdenticalCalls)
		require.NotNil(t, p)
		// Values are 1..20, sorted. P50 of 1..20 = 10.5
		assert.InDelta(t, 10.5, p.P50, 0.1)
	})

	t.Run("unknown key returns nil", func(t *testing.T) {
		unknownKey := ProfileKey{RepoPattern: "nope", Engine: "nope", TaskType: "nope"}
		p := cal.GetPercentiles(ctx, unknownKey, SignalTokenRate)
		assert.Nil(t, p)
	})
}

func TestCalibrator_AllKeys(t *testing.T) {
	cal := NewCalibrator(calibratorLogger())
	ctx := context.Background()

	cal.Record(ctx, Observation{
		RepoURL:         "repo-a",
		Engine:          "claude-code",
		TaskType:        "bug_fix",
		TokensConsumed:  1000,
		DurationSeconds: 60,
		CompletedAt:     time.Now(),
	})
	cal.Record(ctx, Observation{
		RepoURL:         "repo-b",
		Engine:          "codex",
		TaskType:        "feature",
		TokensConsumed:  2000,
		DurationSeconds: 120,
		CompletedAt:     time.Now(),
	})

	keys := cal.AllKeys()
	assert.Len(t, keys, 2)
}

func TestCalibrator_ZeroDuration(t *testing.T) {
	cal := NewCalibrator(calibratorLogger())
	ctx := context.Background()

	// A zero-duration observation should not panic or produce NaN.
	cal.Record(ctx, Observation{
		RepoURL:         "repo",
		Engine:          "claude-code",
		TaskType:        "fix",
		TokensConsumed:  5000,
		DurationSeconds: 0, // edge case
		CompletedAt:     time.Now(),
	})

	key := ProfileKey{RepoPattern: "repo", Engine: "claude-code", TaskType: "fix"}
	p := cal.GetPercentiles(ctx, key, SignalTokenRate)
	require.NotNil(t, p)
	assert.False(t, p.P50 != p.P50, "should not be NaN") // NaN != NaN
}
