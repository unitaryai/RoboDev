package tournament

import (
	"fmt"
	"strings"
)

// JudgeDecision is the structured output expected from the judge run.
type JudgeDecision struct {
	// WinnerIndex is the zero-based index into the candidates slice.
	WinnerIndex int `json:"winner_index"`
	// Reasoning explains why this candidate was chosen.
	Reasoning string `json:"reasoning"`
}

// JudgePromptBuilder constructs prompts for the judge engine.
type JudgePromptBuilder struct{}

// NewJudgePromptBuilder creates a new JudgePromptBuilder.
func NewJudgePromptBuilder() *JudgePromptBuilder {
	return &JudgePromptBuilder{}
}

// BuildPrompt constructs the judge prompt from the task description and
// candidate results. The prompt presents all candidates side-by-side with
// their diffs, summaries, costs, and durations, then asks the judge to
// select the best one.
func (b *JudgePromptBuilder) BuildPrompt(
	taskDescription string,
	candidates []*CandidateResult,
) (string, error) {
	if len(candidates) == 0 {
		return "", fmt.Errorf("no candidates to judge")
	}
	if len(candidates) == 1 {
		return "", fmt.Errorf("need at least 2 candidates for judgement")
	}

	var sb strings.Builder

	sb.WriteString("# Tournament Judge Evaluation\n\n")
	sb.WriteString("You are a code review judge. Your task is to evaluate multiple solutions ")
	sb.WriteString("to the same programming task and select the best one.\n\n")

	sb.WriteString("## Task Description\n\n")
	sb.WriteString(taskDescription)
	sb.WriteString("\n\n")

	sb.WriteString("## Scoring Rubric\n\n")
	sb.WriteString("Evaluate each candidate on:\n")
	sb.WriteString("1. **Correctness**: Does the solution correctly address the task?\n")
	sb.WriteString("2. **Code quality**: Is the code clean, well-structured, and maintainable?\n")
	sb.WriteString("3. **Completeness**: Does it handle edge cases and include tests?\n")
	sb.WriteString("4. **Efficiency**: Is the solution resource-efficient (cost, tokens, time)?\n")
	sb.WriteString("5. **Safety**: Does it avoid security vulnerabilities and follow best practices?\n\n")

	sb.WriteString("## Candidates\n\n")

	for i, c := range candidates {
		sb.WriteString(fmt.Sprintf("### Candidate %d (Engine: %s)\n\n", i, c.Engine))
		sb.WriteString(fmt.Sprintf("- **Success**: %t\n", c.Success))
		sb.WriteString(fmt.Sprintf("- **Cost**: $%.4f\n", c.Cost))
		sb.WriteString(fmt.Sprintf("- **Duration**: %s\n", c.Duration.String()))
		if len(c.PRMScores) > 0 {
			sb.WriteString(fmt.Sprintf("- **PRM Scores**: %v\n", c.PRMScores))
		}
		sb.WriteString(fmt.Sprintf("- **Summary**: %s\n\n", c.Summary))

		if c.Diff != "" {
			sb.WriteString("**Diff:**\n```diff\n")
			// Truncate very long diffs to avoid exceeding context limits.
			diff := c.Diff
			if len(diff) > 50000 {
				diff = diff[:50000] + "\n... (truncated)"
			}
			sb.WriteString(diff)
			sb.WriteString("\n```\n\n")
		} else {
			sb.WriteString("**Diff:** (not available)\n\n")
		}
	}

	sb.WriteString("## Instructions\n\n")
	sb.WriteString("Select the best candidate by responding with a JSON object:\n")
	sb.WriteString("```json\n")
	sb.WriteString(`{"winner_index": <0-based index>, "reasoning": "<your reasoning>"}`)
	sb.WriteString("\n```\n\n")
	sb.WriteString("Consider all rubric criteria. If multiple candidates are equally good, ")
	sb.WriteString("prefer the one with lower cost and faster completion.\n")

	return sb.String(), nil
}
