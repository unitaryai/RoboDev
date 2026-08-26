package memory

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/unitaryai/osmia/internal/taskrun"
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

// TestQueryForTaskIsolatesTenants is the end-to-end check that tenant
// scoping actually keeps one flow's learned knowledge out of another's
// prompt. The graph holds a fact per tenant plus an untenanted one, and each
// query must see only its own.
func TestQueryForTaskIsolatesTenants(t *testing.T) {
	graph := NewGraph(nil, slog.Default())
	ctx := context.Background()

	add := func(id, tenant, content string) {
		require.NoError(t, graph.AddNode(ctx, &Fact{
			ID:         id,
			Content:    content,
			FactKind:   FactTypeSuccessPattern,
			ValidFrom:  time.Now(),
			Confidence: 0.9,
			TenantID:   tenant,
		}))
	}
	add("f-ticketing", taskrun.TenantTicketing, "ticketing flow knowledge")
	add("f-incident", taskrun.TenantIncidentTriage, "incident flow knowledge")
	add("f-legacy", "", "knowledge from before tenanting")

	qe := NewQueryEngine(graph, slog.Default())

	seen := func(tenant string) []string {
		mc, err := qe.QueryForTask(ctx, "some task", "https://example.com/repo", "claude-code", tenant)
		require.NoError(t, err)
		require.NotNil(t, mc)
		var out []string
		for _, f := range mc.RelevantFacts {
			out = append(out, f.Content)
		}
		// The rendered section is what actually reaches the prompt, so
		// assert against it too rather than only the structured facts.
		out = append(out, mc.FormattedSection)
		return out
	}

	ticketing := strings.Join(seen(taskrun.TenantTicketing), "\n")
	assert.Contains(t, ticketing, "ticketing flow knowledge")
	assert.NotContains(t, ticketing, "incident flow knowledge",
		"the ticketing flow must not be shown the incident flow's facts")

	incident := strings.Join(seen(taskrun.TenantIncidentTriage), "\n")
	assert.Contains(t, incident, "incident flow knowledge")
	assert.NotContains(t, incident, "ticketing flow knowledge")

	// A fact stored before tenanting existed belongs to no tenant, so a
	// tenanted query no longer surfaces it. That is the migration cost of
	// this change and is called out in the changelog rather than papered
	// over with a match-anything fallback, which would defeat the isolation.
	assert.NotContains(t, ticketing, "knowledge from before tenanting")
}
