package prm

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeScore(score int) StepScore {
	return StepScore{
		Score:     score,
		Reasoning: "test",
		Timestamp: time.Now(),
	}
}

func TestTrajectory_AddScore_and_Len(t *testing.T) {
	traj := NewTrajectory(5)
	assert.Equal(t, 0, traj.Len())

	traj.AddScore(makeScore(5))
	assert.Equal(t, 1, traj.Len())

	// Fill beyond max length.
	for i := 0; i < 10; i++ {
		traj.AddScore(makeScore(i))
	}
	assert.Equal(t, 5, traj.Len(), "should trim to max length")
}

func TestTrajectory_Latest(t *testing.T) {
	traj := NewTrajectory(50)
	assert.Nil(t, traj.Latest())

	traj.AddScore(makeScore(3))
	traj.AddScore(makeScore(7))

	latest := traj.Latest()
	require.NotNil(t, latest)
	assert.Equal(t, 7, latest.Score)
}

func TestTrajectory_Pattern(t *testing.T) {
	tests := []struct {
		name    string
		scores  []int
		pattern TrajectoryPattern
	}{
		{
			name:    "too few scores returns none",
			scores:  []int{5, 4},
			pattern: PatternNone,
		},
		{
			name:    "sustained decline with 3 drops",
			scores:  []int{8, 7, 5, 3},
			pattern: PatternSustainedDecline,
		},
		{
			name:    "sustained decline with 4 drops",
			scores:  []int{10, 8, 6, 4, 2},
			pattern: PatternSustainedDecline,
		},
		{
			name:    "recovery with 3 increases",
			scores:  []int{2, 4, 6, 8},
			pattern: PatternRecovery,
		},
		{
			name:    "plateau with 5 identical",
			scores:  []int{5, 5, 5, 5, 5},
			pattern: PatternPlateau,
		},
		{
			name:    "oscillation with 4 alternations",
			scores:  []int{3, 6, 3, 6, 3},
			pattern: PatternOscillation,
		},
		{
			name:    "no clear pattern",
			scores:  []int{5, 6, 5, 6},
			pattern: PatternNone,
		},
		{
			name:    "decline takes priority over oscillation",
			scores:  []int{8, 7, 5, 3},
			pattern: PatternSustainedDecline,
		},
		{
			name:    "stable mixed scores",
			scores:  []int{5, 6, 7, 6, 5},
			pattern: PatternNone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			traj := NewTrajectory(50)
			for _, s := range tt.scores {
				traj.AddScore(makeScore(s))
			}
			assert.Equal(t, tt.pattern, traj.Pattern())
		})
	}
}

func TestTrajectory_CurrentTrend(t *testing.T) {
	tests := []struct {
		name   string
		scores []int
		trend  Trend
	}{
		{
			name:   "single score is stable",
			scores: []int{5},
			trend:  TrendStable,
		},
		{
			name:   "declining",
			scores: []int{7, 5},
			trend:  TrendDeclining,
		},
		{
			name:   "improving",
			scores: []int{3, 6},
			trend:  TrendImproving,
		},
		{
			name:   "same score is stable",
			scores: []int{5, 5},
			trend:  TrendStable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			traj := NewTrajectory(50)
			for _, s := range tt.scores {
				traj.AddScore(makeScore(s))
			}
			assert.Equal(t, tt.trend, traj.CurrentTrend())
		})
	}
}

func TestTrajectory_DefaultMaxLength(t *testing.T) {
	traj := NewTrajectory(0)
	assert.NotNil(t, traj)
	// Should use default of 50, so we can add many scores.
	for i := 0; i < 100; i++ {
		traj.AddScore(makeScore(i % 10))
	}
	assert.Equal(t, 50, traj.Len())
}
