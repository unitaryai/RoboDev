package tournament

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTournament_CompletedCount(t *testing.T) {
	tests := []struct {
		name    string
		results map[string]*CandidateResult
		want    int
	}{
		{
			name:    "empty results",
			results: map[string]*CandidateResult{},
			want:    0,
		},
		{
			name: "nil entries not counted",
			results: map[string]*CandidateResult{
				"tr-1": nil,
				"tr-2": {TaskRunID: "tr-2"},
			},
			want: 1,
		},
		{
			name: "all completed",
			results: map[string]*CandidateResult{
				"tr-1": {TaskRunID: "tr-1"},
				"tr-2": {TaskRunID: "tr-2"},
			},
			want: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tournament := &Tournament{CandidateResults: tt.results}
			assert.Equal(t, tt.want, tournament.CompletedCount())
		})
	}
}

func TestTournament_IsReadyForJudging(t *testing.T) {
	tests := []struct {
		name      string
		total     int
		completed int
		threshold float64
		judging   bool
		want      bool
	}{
		{
			name:      "no candidates",
			total:     0,
			completed: 0,
			threshold: 0.6,
			want:      false,
		},
		{
			name:      "already judging",
			total:     3,
			completed: 2,
			threshold: 0.6,
			judging:   true,
			want:      false,
		},
		{
			name:      "below threshold",
			total:     3,
			completed: 1,
			threshold: 0.6,
			want:      false,
		},
		{
			name:      "at threshold 60% of 3 rounds up to 2",
			total:     3,
			completed: 2,
			threshold: 0.6,
			want:      true,
		},
		{
			name:      "all completed",
			total:     3,
			completed: 3,
			threshold: 0.6,
			want:      true,
		},
		{
			name:      "zero threshold defaults to 60%",
			total:     3,
			completed: 2,
			threshold: 0,
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ids := make([]string, tt.total)
			states := make(map[string]TournamentState, tt.total)
			results := make(map[string]*CandidateResult, tt.total)
			for i := 0; i < tt.total; i++ {
				id := "tr-" + string(rune('1'+i))
				ids[i] = id
				states[id] = StateCompeting
				if i < tt.completed {
					results[id] = &CandidateResult{TaskRunID: id}
				}
			}

			tournament := &Tournament{
				TaskRunIDs:       ids,
				CandidateStates:  states,
				CandidateResults: results,
				Config: TournamentConfig{
					EarlyTerminationThreshold: tt.threshold,
				},
			}
			if tt.judging {
				tournament.JudgeTaskRunID = "judge-1"
			}

			assert.Equal(t, tt.want, tournament.IsReadyForJudging())
		})
	}
}

func TestTournament_CompletedResults(t *testing.T) {
	tournament := &Tournament{
		TaskRunIDs: []string{"tr-1", "tr-2", "tr-3"},
		CandidateResults: map[string]*CandidateResult{
			"tr-1": {TaskRunID: "tr-1", Engine: "claude-code"},
			"tr-3": {TaskRunID: "tr-3", Engine: "aider"},
		},
	}

	results := tournament.CompletedResults()
	assert.Len(t, results, 2)
	assert.Equal(t, "tr-1", results[0].TaskRunID)
	assert.Equal(t, "tr-3", results[1].TaskRunID)
}

func TestTournament_LaggingCandidateIDs(t *testing.T) {
	tournament := &Tournament{
		TaskRunIDs: []string{"tr-1", "tr-2", "tr-3"},
		CandidateResults: map[string]*CandidateResult{
			"tr-1": {TaskRunID: "tr-1"},
		},
	}

	lagging := tournament.LaggingCandidateIDs()
	assert.Len(t, lagging, 2)
	assert.Contains(t, lagging, "tr-2")
	assert.Contains(t, lagging, "tr-3")
}

func TestCandidateResult_Fields(t *testing.T) {
	r := &CandidateResult{
		TaskRunID: "tr-1",
		Engine:    "claude-code",
		Success:   true,
		Cost:      1.50,
		Duration:  5 * time.Minute,
		PRMScores: []int{8, 7, 9},
	}

	assert.Equal(t, "tr-1", r.TaskRunID)
	assert.Equal(t, "claude-code", r.Engine)
	assert.True(t, r.Success)
	assert.InDelta(t, 1.50, r.Cost, 0.001)
	assert.Equal(t, 5*time.Minute, r.Duration)
	assert.Equal(t, []int{8, 7, 9}, r.PRMScores)
}
