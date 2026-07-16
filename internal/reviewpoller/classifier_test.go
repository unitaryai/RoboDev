package reviewpoller

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/unitaryai/osmia/pkg/plugin/scm"
)

// TestRuleBasedClassifier_Classify exercises the keyword and author-pattern
// heuristics of RuleBasedClassifier via table-driven subtests.
func TestRuleBasedClassifier_Classify(t *testing.T) {
	tests := []struct {
		name          string
		extraPatterns []string
		comment       scm.ReviewComment
		wantClass     Classification
		wantSeverity  string
	}{
		{
			name:         "empty body is ignored",
			comment:      scm.ReviewComment{ID: "1", Author: "alice", Body: "   "},
			wantClass:    ClassificationIgnore,
			wantSeverity: "info",
		},
		{
			name:         "non-inline built-in bot username summary is ignored",
			comment:      scm.ReviewComment{ID: "2", Author: "coderabbit-ai", Body: "You should fix the error handling."},
			wantClass:    ClassificationIgnore,
			wantSeverity: "info",
		},
		{
			name:         "non-inline built-in GitLab group-bot pattern is ignored",
			comment:      scm.ReviewComment{ID: "3", Author: "group_101508187_bot_f1eac3692eaf8315c51fba127e720935", Body: "You should fix the error handling."},
			wantClass:    ClassificationIgnore,
			wantSeverity: "info",
		},
		{
			name: "inline comment from same bot author is still evaluated",
			comment: scm.ReviewComment{
				ID: "4", Author: "coderabbit-ai", Body: "You should add error handling here.",
				FilePath: "pkg/handler.go", Line: 42,
			},
			// The body contains the substring "error", which is checked
			// against the error-keyword tier before the warning tier.
			wantClass:    ClassificationRequiresAction,
			wantSeverity: "error",
		},
		{
			name:          "custom IgnoreSummaryAuthors pattern ignores non-inline comment",
			extraPatterns: []string{`^custom-bot-\d+$`},
			comment:       scm.ReviewComment{ID: "5", Author: "custom-bot-42", Body: "You should fix the error handling."},
			wantClass:     ClassificationIgnore,
			wantSeverity:  "info",
		},
		{
			name:          "custom IgnoreSummaryAuthors pattern still evaluates inline comment",
			extraPatterns: []string{`^custom-bot-\d+$`},
			comment: scm.ReviewComment{
				ID: "6", Author: "custom-bot-42", Body: "You should add a nil check here.",
				FilePath: "main.go", Line: 10,
			},
			wantClass:    ClassificationRequiresAction,
			wantSeverity: "warning",
		},
		{
			name:         "error keyword classifies as requires-action/error",
			comment:      scm.ReviewComment{ID: "7", Author: "bob", Body: "This will crash in production."},
			wantClass:    ClassificationRequiresAction,
			wantSeverity: "error",
		},
		{
			name:         "warning keyword classifies as requires-action/warning",
			comment:      scm.ReviewComment{ID: "8", Author: "bob", Body: "Please consider renaming this variable."},
			wantClass:    ClassificationRequiresAction,
			wantSeverity: "warning",
		},
		{
			name:         "informational keyword classifies as informational",
			comment:      scm.ReviewComment{ID: "9", Author: "bob", Body: "LGTM, thanks!"},
			wantClass:    ClassificationInformational,
			wantSeverity: "info",
		},
		{
			name:         "no keyword match falls back to informational",
			comment:      scm.ReviewComment{ID: "10", Author: "bob", Body: "Interesting approach here."},
			wantClass:    ClassificationInformational,
			wantSeverity: "info",
		},
		{
			name:         "empty author with actionable body is still classified by keyword",
			comment:      scm.ReviewComment{ID: "11", Author: "", Body: "This is broken, please fix it."},
			wantClass:    ClassificationRequiresAction,
			wantSeverity: "error",
		},
		{
			name:         "mixed-case body keyword still matches",
			comment:      scm.ReviewComment{ID: "12", Author: "bob", Body: "This is BROKEN and needs a Fix."},
			wantClass:    ClassificationRequiresAction,
			wantSeverity: "error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			classifier := NewRuleBasedClassifier(tt.extraPatterns)
			got, err := classifier.Classify(context.Background(), tt.comment)
			require.NoError(t, err)
			assert.Equal(t, tt.wantClass, got.Classification)
			assert.Equal(t, tt.wantSeverity, got.Severity)
		})
	}
}

// TestNewRuleBasedClassifier_InvalidExtraPatternSkipped verifies that a
// malformed regex in extraPatterns is silently dropped rather than causing
// construction to panic or error, and that the malformed pattern has no
// effect on classification.
func TestNewRuleBasedClassifier_InvalidExtraPatternSkipped(t *testing.T) {
	require.NotPanics(t, func() {
		classifier := NewRuleBasedClassifier([]string{"(unterminated["})
		assert.NotNil(t, classifier)

		// The malformed pattern should not match anything; a non-inline
		// comment from an unrelated author should still be classified
		// normally by keyword, not silently ignored.
		got, err := classifier.Classify(context.Background(), scm.ReviewComment{
			ID: "1", Author: "alice", Body: "please fix this",
		})
		require.NoError(t, err)
		assert.Equal(t, ClassificationRequiresAction, got.Classification)
	})
}

// TestPrefilterBotSummary directly exercises the unexported prefilterBotSummary
// helper, independent of the wider Classify keyword logic.
func TestPrefilterBotSummary(t *testing.T) {
	patterns := NewRuleBasedClassifier(nil).summaryAuthorPatterns

	t.Run("inline comment is never filtered even for a bot author", func(t *testing.T) {
		comment := scm.ReviewComment{
			ID: "1", Author: "coderabbit-ai", Body: "please fix this",
			FilePath: "main.go", Line: 1, Created: time.Now(),
		}
		_, skipped := prefilterBotSummary(comment, patterns)
		assert.False(t, skipped)
	})

	t.Run("non-inline comment from unmatched author is not filtered", func(t *testing.T) {
		comment := scm.ReviewComment{
			ID: "2", Author: "alice", Body: "please fix this", Created: time.Now(),
		}
		_, skipped := prefilterBotSummary(comment, patterns)
		assert.False(t, skipped)
	})

	t.Run("non-inline comment from matched author is filtered", func(t *testing.T) {
		comment := scm.ReviewComment{
			ID: "3", Author: "dependabot[bot]", Body: "bumps foo from 1 to 2", Created: time.Now(),
		}
		result, skipped := prefilterBotSummary(comment, patterns)
		assert.True(t, skipped)
		assert.Equal(t, ClassificationIgnore, result.Classification)
	})
}
