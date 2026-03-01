package memory

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

func TestGraph_AddNode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		node    Node
		wantErr string
	}{
		{
			name: "valid fact node",
			node: &Fact{
				ID:         "fact-1",
				Content:    "test fact",
				Confidence: 0.9,
				DecayRate:  0.01,
				ValidFrom:  time.Now(),
			},
		},
		{
			name: "valid pattern node",
			node: &Pattern{
				ID:          "pattern-1",
				Description: "test pattern",
				Confidence:  0.8,
				DecayRate:   0.02,
				FirstSeen:   time.Now(),
			},
		},
		{
			name: "valid engine profile node",
			node: &EngineProfile{
				ID:          "ep-1",
				EngineName:  "claude-code",
				SuccessRate: map[string]float64{"bug_fix": 0.85},
				Confidence:  0.7,
				DecayRate:   0.01,
				ValidFrom:   time.Now(),
			},
		},
		{
			name:    "nil node returns error",
			node:    nil,
			wantErr: "cannot add nil node",
		},
		{
			name: "empty id returns error",
			node: &Fact{
				ID:         "",
				Content:    "no id",
				Confidence: 0.5,
			},
			wantErr: "node must have a non-empty id",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g := NewGraph(nil, testLogger())
			err := g.AddNode(context.Background(), tt.node)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, 1, g.NodeCount())
			assert.NotNil(t, g.GetNode(tt.node.NodeID()))
		})
	}
}

func TestGraph_AddEdge(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		edge    Edge
		wantErr string
	}{
		{
			name: "valid edge",
			edge: Edge{
				FromID:    "a",
				ToID:      "b",
				Relation:  RelationRelatesTo,
				Weight:    1.0,
				CreatedAt: time.Now(),
			},
		},
		{
			name: "empty from_id returns error",
			edge: Edge{
				FromID: "",
				ToID:   "b",
			},
			wantErr: "non-empty from_id and to_id",
		},
		{
			name: "empty to_id returns error",
			edge: Edge{
				FromID: "a",
				ToID:   "",
			},
			wantErr: "non-empty from_id and to_id",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g := NewGraph(nil, testLogger())
			err := g.AddEdge(context.Background(), tt.edge)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, 1, g.EdgeCount())
		})
	}
}

func TestGraph_Query(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	g := NewGraph(nil, testLogger())

	// Add nodes with varying confidence and age.
	now := time.Now()
	require.NoError(t, g.AddNode(ctx, &Fact{
		ID:         "recent-high",
		Content:    "recent high confidence",
		Confidence: 0.95,
		DecayRate:  0.01,
		ValidFrom:  now,
		TenantID:   "tenant-a",
	}))
	require.NoError(t, g.AddNode(ctx, &Fact{
		ID:         "old-high",
		Content:    "old high confidence",
		Confidence: 0.95,
		DecayRate:  0.01,
		ValidFrom:  now.Add(-720 * time.Hour), // 30 days old
		TenantID:   "tenant-a",
	}))
	require.NoError(t, g.AddNode(ctx, &Fact{
		ID:         "recent-low",
		Content:    "recent low confidence",
		Confidence: 0.3,
		DecayRate:  0.01,
		ValidFrom:  now,
		TenantID:   "tenant-a",
	}))
	require.NoError(t, g.AddNode(ctx, &Fact{
		ID:         "other-tenant",
		Content:    "different tenant",
		Confidence: 1.0,
		DecayRate:  0.01,
		ValidFrom:  now,
		TenantID:   "tenant-b",
	}))

	tests := []struct {
		name      string
		query     GraphQuery
		wantFirst string
		wantCount int
	}{
		{
			name: "tenant isolation",
			query: GraphQuery{
				TenantID:   "tenant-a",
				MaxResults: 10,
			},
			wantFirst: "recent-high",
			wantCount: 3,
		},
		{
			name: "different tenant sees own nodes",
			query: GraphQuery{
				TenantID:   "tenant-b",
				MaxResults: 10,
			},
			wantFirst: "other-tenant",
			wantCount: 1,
		},
		{
			name: "max results limits output",
			query: GraphQuery{
				TenantID:   "tenant-a",
				MaxResults: 1,
			},
			wantFirst: "recent-high",
			wantCount: 1,
		},
		{
			name: "recent high-confidence nodes score highest",
			query: GraphQuery{
				TenantID:   "tenant-a",
				MaxResults: 10,
			},
			wantFirst: "recent-high",
			wantCount: 3,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			results, err := g.Query(ctx, tt.query)
			require.NoError(t, err)
			assert.Len(t, results, tt.wantCount)
			if tt.wantCount > 0 {
				assert.Equal(t, tt.wantFirst, results[0].NodeID())
			}
		})
	}
}

func TestGraph_DecayConfidence(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	g := NewGraph(nil, testLogger())

	require.NoError(t, g.AddNode(ctx, &Fact{
		ID:         "decayable",
		Content:    "will decay",
		Confidence: 1.0,
		DecayRate:  0.1, // 10% decay per interval
		ValidFrom:  time.Now(),
	}))
	require.NoError(t, g.AddNode(ctx, &Fact{
		ID:         "no-decay",
		Content:    "will not decay",
		Confidence: 1.0,
		DecayRate:  0.0, // no decay
		ValidFrom:  time.Now(),
	}))

	g.DecayConfidence(ctx)

	decayed := g.GetNode("decayable")
	require.NotNil(t, decayed)
	assert.InDelta(t, 0.9, decayed.GetConfidence(), 0.001)

	stable := g.GetNode("no-decay")
	require.NotNil(t, stable)
	assert.InDelta(t, 1.0, stable.GetConfidence(), 0.001)
}

func TestGraph_PruneStale(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	g := NewGraph(nil, testLogger())

	require.NoError(t, g.AddNode(ctx, &Fact{
		ID: "keep", Content: "keep me", Confidence: 0.8, ValidFrom: time.Now(),
	}))
	require.NoError(t, g.AddNode(ctx, &Fact{
		ID: "prune", Content: "prune me", Confidence: 0.05, ValidFrom: time.Now(),
	}))
	require.NoError(t, g.AddEdge(ctx, Edge{
		FromID: "keep", ToID: "prune", Relation: RelationRelatesTo, Weight: 1.0, CreatedAt: time.Now(),
	}))

	assert.Equal(t, 2, g.NodeCount())
	assert.Equal(t, 1, g.EdgeCount())

	pruned := g.PruneStale(ctx, 0.1)
	assert.Equal(t, 1, pruned)
	assert.Equal(t, 1, g.NodeCount())
	assert.Equal(t, 0, g.EdgeCount()) // edge referencing pruned node removed
	assert.Nil(t, g.GetNode("prune"))
	assert.NotNil(t, g.GetNode("keep"))
}

func TestGraph_QueryEngineFilter(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	g := NewGraph(nil, testLogger())

	require.NoError(t, g.AddNode(ctx, &EngineProfile{
		ID:          "ep-claude",
		EngineName:  "claude-code",
		SuccessRate: map[string]float64{"bug_fix": 0.9},
		Confidence:  0.8,
		ValidFrom:   time.Now(),
	}))
	require.NoError(t, g.AddNode(ctx, &EngineProfile{
		ID:          "ep-codex",
		EngineName:  "codex",
		SuccessRate: map[string]float64{"bug_fix": 0.7},
		Confidence:  0.8,
		ValidFrom:   time.Now(),
	}))

	results, err := g.Query(ctx, GraphQuery{Engine: "claude-code", MaxResults: 10})
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "ep-claude", results[0].NodeID())
}
