package tournament

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJudgePromptBuilder_BuildPrompt(t *testing.T) {
	tests := []struct {
		name        string
		task        string
		candidates  []*CandidateResult
		wantErr     bool
		wantContain []string
	}{
		{
			name: "valid two candidates",
			task: "Fix the login bug",
			candidates: []*CandidateResult{
				{
					TaskRunID: "tr-1",
					Engine:    "claude-code",
					Diff:      "--- a/main.go\n+++ b/main.go\n@@ -1 +1 @@\n-old\n+new",
					Summary:   "Fixed the login flow",
					Success:   true,
					Cost:      2.0,
					Duration:  5 * time.Minute,
				},
				{
					TaskRunID: "tr-2",
					Engine:    "aider",
					Diff:      "--- a/main.go\n+++ b/main.go\n@@ -1 +1 @@\n-old\n+better",
					Summary:   "Refactored login",
					Success:   true,
					Cost:      0.8,
					Duration:  3 * time.Minute,
					PRMScores: []int{8, 7, 9},
				},
			},
			wantContain: []string{
				"Tournament Judge",
				"Fix the login bug",
				"Candidate 0",
				"Candidate 1",
				"claude-code",
				"aider",
				"winner_index",
				"Correctness",
				"PRM Scores",
			},
		},
		{
			name:       "empty candidates",
			task:       "Some task",
			candidates: nil,
			wantErr:    true,
		},
		{
			name: "single candidate",
			task: "Some task",
			candidates: []*CandidateResult{
				{TaskRunID: "tr-1", Engine: "claude-code"},
			},
			wantErr: true,
		},
		{
			name: "candidate without diff",
			task: "Some task",
			candidates: []*CandidateResult{
				{TaskRunID: "tr-1", Engine: "claude-code", Summary: "done"},
				{TaskRunID: "tr-2", Engine: "aider", Summary: "also done"},
			},
			wantContain: []string{"not available"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewJudgePromptBuilder()
			prompt, err := builder.BuildPrompt(tt.task, tt.candidates)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.NotEmpty(t, prompt)

			for _, s := range tt.wantContain {
				assert.Contains(t, prompt, s, "prompt should contain %q", s)
			}
		})
	}
}

func TestJudgePromptBuilder_TruncatesLongDiffs(t *testing.T) {
	builder := NewJudgePromptBuilder()

	longDiff := make([]byte, 60000)
	for i := range longDiff {
		longDiff[i] = 'x'
	}

	candidates := []*CandidateResult{
		{TaskRunID: "tr-1", Engine: "a", Diff: string(longDiff)},
		{TaskRunID: "tr-2", Engine: "b", Diff: "short"},
	}

	prompt, err := builder.BuildPrompt("task", candidates)
	require.NoError(t, err)
	assert.Contains(t, prompt, "truncated")
}
