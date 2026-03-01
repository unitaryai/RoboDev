package estimator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComplexityScorer_ScoreDimensions(t *testing.T) {
	scorer := NewComplexityScorer()
	ctx := context.Background()

	tests := []struct {
		name          string
		input         ComplexityInput
		checkOverall  func(float64) bool
		checkDim      string
		checkDimRange [2]float64
	}{
		{
			name: "simple typo fix has low complexity",
			input: ComplexityInput{
				TaskDescription: "Fix typo in readme",
				TaskType:        "typo_fix",
				RepoSize:        50,
				Labels:          []string{"typo-fix"},
			},
			checkOverall:  func(v float64) bool { return v < 0.3 },
			checkDim:      DimTaskTypeComplexity,
			checkDimRange: [2]float64{0.0, 0.2},
		},
		{
			name: "architecture migration has high complexity",
			input: ComplexityInput{
				TaskDescription: "Migrate the authentication system from session-based to JWT tokens.\n- Update all middleware\n- Update all API handlers\n- Migrate existing sessions\n- Update deployment config\n- Add token refresh mechanism\n- Update documentation",
				TaskType:        "migration",
				RepoSize:        8000,
				Labels:          []string{"architecture", "migration"},
			},
			checkOverall:  func(v float64) bool { return v > 0.6 },
			checkDim:      DimRepoSize,
			checkDimRange: [2]float64{0.8, 1.0},
		},
		{
			name: "medium bug fix in medium repo",
			input: ComplexityInput{
				TaskDescription: "The login form does not show error messages when authentication fails. Users see a blank page instead of the error message.",
				TaskType:        "bug_fix",
				RepoSize:        500,
				Labels:          []string{"bug"},
			},
			checkOverall:  func(v float64) bool { return v > 0.2 && v < 0.6 },
			checkDim:      DimLabelComplexity,
			checkDimRange: [2]float64{0.25, 0.4},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score, err := scorer.Score(ctx, tt.input)
			require.NoError(t, err)
			require.NotNil(t, score)

			assert.True(t, tt.checkOverall(score.Overall),
				"overall score %.3f did not meet expectation", score.Overall)

			dimVal, ok := score.Dimensions[tt.checkDim]
			assert.True(t, ok, "missing dimension %s", tt.checkDim)
			assert.GreaterOrEqual(t, dimVal, tt.checkDimRange[0],
				"dimension %s value %.3f below minimum", tt.checkDim, dimVal)
			assert.LessOrEqual(t, dimVal, tt.checkDimRange[1],
				"dimension %s value %.3f above maximum", tt.checkDim, dimVal)
		})
	}
}

func TestScoreDescriptionComplexity(t *testing.T) {
	tests := []struct {
		name     string
		desc     string
		minScore float64
		maxScore float64
	}{
		{"empty description", "", 0.15, 0.25},
		{"short description", "Fix typo", 0.05, 0.2},
		{"medium description", "The login form does not show error messages when auth fails. Users see blank page. Need to add proper error handling and display the error message from the API response.", 0.2, 0.5},
		{
			"long description with bullets",
			"Refactor the entire authentication system:\n- Migrate to JWT\n- Update middleware\n- Update handlers\n- Add refresh tokens\n- Update docs\n- Add integration tests",
			0.3, 0.8,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := scoreDescriptionComplexity(tt.desc)
			assert.GreaterOrEqual(t, score, tt.minScore)
			assert.LessOrEqual(t, score, tt.maxScore)
		})
	}
}

func TestScoreRepoSize(t *testing.T) {
	tests := []struct {
		name     string
		files    int
		expected float64
	}{
		{"tiny", 10, 0.2},
		{"small", 100, 0.2},
		{"medium-low", 300, 0.4},
		{"medium", 500, 0.4},
		{"medium-high", 1000, 0.5},
		{"large", 3000, 0.8},
		{"huge", 10000, 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, scoreRepoSize(tt.files))
		})
	}
}

func TestScoreTaskType(t *testing.T) {
	tests := []struct {
		name     string
		taskType string
		expected float64
	}{
		{"typo_fix", "typo_fix", 0.1},
		{"bug_fix", "bug_fix", 0.35},
		{"refactor", "refactor", 0.65},
		{"unknown", "custom_type", 0.4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.InDelta(t, tt.expected, scoreTaskType(tt.taskType), 0.001)
		})
	}
}

func TestScoreLabelComplexity(t *testing.T) {
	tests := []struct {
		name     string
		labels   []string
		expected float64
	}{
		{"no labels", nil, 0.3},
		{"typo-fix label", []string{"typo-fix"}, 0.3},
		{"architecture label", []string{"architecture"}, 0.85},
		{"multiple labels uses highest", []string{"bug", "security"}, 0.7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.InDelta(t, tt.expected, scoreLabelComplexity(tt.labels), 0.001)
		})
	}
}

func TestComplexityScore_AllDimensionsPresent(t *testing.T) {
	scorer := NewComplexityScorer()
	ctx := context.Background()

	score, err := scorer.Score(ctx, ComplexityInput{
		TaskDescription: "Fix a bug",
		TaskType:        "bug_fix",
		RepoSize:        100,
		Labels:          []string{"bug"},
	})
	require.NoError(t, err)

	expectedDims := []string{DimDescriptionComplexity, DimLabelComplexity, DimRepoSize, DimTaskTypeComplexity}
	for _, dim := range expectedDims {
		_, ok := score.Dimensions[dim]
		assert.True(t, ok, "missing expected dimension %q", dim)
	}

	assert.GreaterOrEqual(t, score.Overall, 0.0)
	assert.LessOrEqual(t, score.Overall, 1.0)
}
