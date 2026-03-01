package routing

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValueStats_SuccessRate(t *testing.T) {
	tests := []struct {
		name      string
		successes int
		failures  int
		expected  float64
	}{
		{
			name:      "zero samples gives 0.5 (Laplace prior)",
			successes: 0,
			failures:  0,
			expected:  0.5,
		},
		{
			name:      "all successes converges toward 1.0",
			successes: 100,
			failures:  0,
			expected:  101.0 / 102.0,
		},
		{
			name:      "all failures converges toward 0.0",
			successes: 0,
			failures:  100,
			expected:  1.0 / 102.0,
		},
		{
			name:      "equal successes and failures gives ~0.5",
			successes: 50,
			failures:  50,
			expected:  51.0 / 102.0,
		},
		{
			name:      "single success",
			successes: 1,
			failures:  0,
			expected:  2.0 / 3.0,
		},
		{
			name:      "single failure",
			successes: 0,
			failures:  1,
			expected:  1.0 / 3.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vs := &ValueStats{Successes: tt.successes, Failures: tt.failures}
			assert.InDelta(t, tt.expected, vs.SuccessRate(), 0.0001)
		})
	}
}

func TestDimensionStats_SuccessRate(t *testing.T) {
	ds := &DimensionStats{Successes: 8, Failures: 2}
	// (8+1)/(8+2+2) = 9/12 = 0.75
	assert.InDelta(t, 0.75, ds.SuccessRate(), 0.0001)
}

func TestEngineFingerprint_Update(t *testing.T) {
	fp := NewEngineFingerprint("claude-code")

	outcome := TaskOutcome{
		EngineName:   "claude-code",
		TaskType:     "bug_fix",
		RepoLanguage: "go",
		RepoSize:     50,
		Complexity:   "low",
		Success:      true,
		Duration:     5 * time.Minute,
		Cost:         1.50,
	}

	fp.Update(outcome)

	require.Equal(t, 1, fp.TotalTasks)

	// Check task_type dimension.
	ds := fp.Dimensions[DimensionTaskType]
	require.NotNil(t, ds)
	assert.Equal(t, 1, ds.Successes)
	assert.Equal(t, 0, ds.Failures)

	vs, ok := ds.Values["bug_fix"]
	require.True(t, ok)
	assert.Equal(t, 1, vs.Successes)
	assert.Equal(t, 0, vs.Failures)

	// Check repo_size_bucket dimension (50 files => "small").
	ds = fp.Dimensions[DimensionRepoSize]
	require.NotNil(t, ds)
	_, ok = ds.Values["small"]
	assert.True(t, ok)
}

func TestEngineFingerprint_UpdateFailure(t *testing.T) {
	fp := NewEngineFingerprint("codex")

	fp.Update(TaskOutcome{
		EngineName: "codex",
		TaskType:   "refactor",
		Success:    false,
	})

	ds := fp.Dimensions[DimensionTaskType]
	require.NotNil(t, ds)
	assert.Equal(t, 0, ds.Successes)
	assert.Equal(t, 1, ds.Failures)

	vs := ds.Values["refactor"]
	require.NotNil(t, vs)
	assert.Equal(t, 0, vs.Successes)
	assert.Equal(t, 1, vs.Failures)
}

func TestEngineFingerprint_Score(t *testing.T) {
	fp := NewEngineFingerprint("claude-code")

	// Add 10 successful bug_fix tasks and 2 failed refactor tasks
	// to create differentiation between task types.
	for i := 0; i < 10; i++ {
		fp.Update(TaskOutcome{
			EngineName:   "claude-code",
			TaskType:     "bug_fix",
			RepoLanguage: "go",
			RepoSize:     50,
			Complexity:   "low",
			Success:      true,
		})
	}
	for i := 0; i < 2; i++ {
		fp.Update(TaskOutcome{
			EngineName:   "claude-code",
			TaskType:     "refactor",
			RepoLanguage: "go",
			RepoSize:     50,
			Complexity:   "low",
			Success:      false,
		})
	}

	// Exact match on bug_fix should score higher than refactor.
	bugFixScore := fp.Score(RoutingQuery{
		TaskType:     "bug_fix",
		RepoLanguage: "go",
		RepoSize:     50,
		Complexity:   "low",
	})

	refactorScore := fp.Score(RoutingQuery{
		TaskType:     "refactor",
		RepoLanguage: "go",
		RepoSize:     50,
		Complexity:   "low",
	})

	assert.Greater(t, bugFixScore, refactorScore,
		"bug_fix (all successes) should score higher than refactor (all failures)")

	// Completely unknown query still returns a valid score > 0.
	unknownScore := fp.Score(RoutingQuery{
		TaskType:     "security_audit",
		RepoLanguage: "rust",
		RepoSize:     5000,
		Complexity:   "high",
	})
	assert.Greater(t, unknownScore, 0.0)
}

func TestEngineFingerprint_ScoreEmptyFingerprint(t *testing.T) {
	fp := NewEngineFingerprint("empty-engine")

	score := fp.Score(RoutingQuery{
		TaskType:     "bug_fix",
		RepoLanguage: "python",
	})

	// With no data, each dimension falls back to Laplace prior: (0+1)/(0+0+2) = 0.5
	// 4 dimensions: 0.5^4 = 0.0625
	assert.InDelta(t, 0.0625, score, 0.0001)
}

func TestRepoSizeBucket(t *testing.T) {
	tests := []struct {
		name     string
		files    int
		expected string
	}{
		{"tiny repo", 10, "small"},
		{"small boundary", 100, "small"},
		{"medium repo", 500, "medium"},
		{"medium boundary", 1000, "medium"},
		{"large repo", 5000, "large"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, repoSizeBucket(tt.files))
		})
	}
}
