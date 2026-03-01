package memory

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryEngine_QueryForTask(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	g := NewGraph(nil, testLogger())

	now := time.Now()

	// Populate graph with test data.
	require.NoError(t, g.AddNode(ctx, &Fact{
		ID:         "success-1",
		Content:    "claude-code succeeded on bug fix",
		Source:     "tr-100",
		FactKind:   FactTypeSuccessPattern,
		ValidFrom:  now,
		Confidence: 0.9,
		DecayRate:  0.01,
		TenantID:   "tenant-a",
	}))
	require.NoError(t, g.AddNode(ctx, &Fact{
		ID:         "failure-1",
		Content:    "codex failed on feature task: compilation error",
		Source:     "tr-101",
		FactKind:   FactTypeFailurePattern,
		ValidFrom:  now.Add(-48 * time.Hour),
		Confidence: 0.85,
		DecayRate:  0.02,
		TenantID:   "tenant-a",
	}))
	require.NoError(t, g.AddNode(ctx, &EngineProfile{
		ID:          "ep-claude",
		EngineName:  "claude-code",
		SuccessRate: map[string]float64{"bug_fix": 0.9, "feature": 0.75},
		Strengths:   []string{"fast"},
		Weaknesses:  []string{"expensive"},
		Confidence:  0.8,
		DecayRate:   0.01,
		ValidFrom:   now,
	}))
	require.NoError(t, g.AddNode(ctx, &Pattern{
		ID:          "pattern-1",
		Description: "heavy Bash usage during complex tasks",
		Occurrences: 15,
		FirstSeen:   now.Add(-72 * time.Hour),
		LastSeen:    now,
		Confidence:  0.7,
		DecayRate:   0.02,
		TenantID:    "tenant-a",
	}))

	tests := []struct {
		name        string
		tenantID    string
		engine      string
		wantFacts   int
		wantIssues  int
		wantSection bool
	}{
		{
			name:        "retrieves facts for tenant",
			tenantID:    "tenant-a",
			engine:      "",
			wantFacts:   2,
			wantIssues:  1,
			wantSection: true,
		},
		{
			name:        "empty tenant returns all nodes",
			tenantID:    "",
			engine:      "claude-code",
			wantFacts:   2,
			wantIssues:  1,
			wantSection: true, // engine profile still returns insights
		},
		{
			name:        "unknown tenant returns empty",
			tenantID:    "tenant-z",
			engine:      "",
			wantFacts:   0,
			wantIssues:  0,
			wantSection: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			qe := NewQueryEngine(g, testLogger())
			mc, err := qe.QueryForTask(ctx, "fix a bug", "https://github.com/test/repo", tt.engine, tt.tenantID)
			require.NoError(t, err)
			require.NotNil(t, mc)

			assert.Len(t, mc.RelevantFacts, tt.wantFacts)
			assert.Len(t, mc.KnownIssues, tt.wantIssues)

			if tt.wantSection {
				assert.NotEmpty(t, mc.FormattedSection)
				assert.Contains(t, mc.FormattedSection, "## Prior Knowledge")
			} else {
				assert.Empty(t, mc.FormattedSection)
			}
		})
	}
}

func TestQueryEngine_FormattedSection(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	g := NewGraph(nil, testLogger())

	require.NoError(t, g.AddNode(ctx, &Fact{
		ID:         "f-1",
		Content:    "important fact",
		Source:     "tr-200",
		FactKind:   FactTypeSuccessPattern,
		ValidFrom:  time.Now(),
		Confidence: 0.9,
		DecayRate:  0.01,
		TenantID:   "t1",
	}))
	require.NoError(t, g.AddNode(ctx, &Fact{
		ID:         "f-2",
		Content:    "known problem with large repos",
		Source:     "tr-201",
		FactKind:   FactTypeFailurePattern,
		ValidFrom:  time.Now(),
		Confidence: 0.8,
		DecayRate:  0.01,
		TenantID:   "t1",
	}))

	qe := NewQueryEngine(g, testLogger())
	mc, err := qe.QueryForTask(ctx, "test task", "https://example.com/repo", "", "t1")
	require.NoError(t, err)

	assert.Contains(t, mc.FormattedSection, "## Prior Knowledge")
	assert.Contains(t, mc.FormattedSection, "### Relevant Facts")
	assert.Contains(t, mc.FormattedSection, "important fact")
	assert.Contains(t, mc.FormattedSection, "confidence: 90%")
	assert.Contains(t, mc.FormattedSection, "### Known Issues")
	assert.Contains(t, mc.FormattedSection, "known problem with large repos")
}

func TestQueryEngine_TemporalWeighting(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	g := NewGraph(nil, testLogger())

	now := time.Now()

	// Add two facts: one recent, one old. Both have the same confidence.
	require.NoError(t, g.AddNode(ctx, &Fact{
		ID: "recent", Content: "recent fact", Confidence: 0.8,
		DecayRate: 0.01, ValidFrom: now, TenantID: "t",
	}))
	require.NoError(t, g.AddNode(ctx, &Fact{
		ID: "old", Content: "old fact", Confidence: 0.8,
		DecayRate: 0.01, ValidFrom: now.Add(-1440 * time.Hour), TenantID: "t",
	}))

	qe := NewQueryEngine(g, testLogger())
	mc, err := qe.QueryForTask(ctx, "test", "", "", "t")
	require.NoError(t, err)
	require.Len(t, mc.RelevantFacts, 2)

	// Recent fact should appear first due to temporal weighting.
	assert.Equal(t, "recent", mc.RelevantFacts[0].ID)
	assert.Equal(t, "old", mc.RelevantFacts[1].ID)
}
