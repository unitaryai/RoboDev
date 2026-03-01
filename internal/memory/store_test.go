package memory

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	store, err := NewSQLiteStore(":memory:", testLogger())
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	return store
}

func TestSQLiteStore_SaveAndListNodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		node     Node
		wantType NodeType
	}{
		{
			name: "save and retrieve fact",
			node: &Fact{
				ID:         "fact-store-1",
				Content:    "test fact for store",
				Source:     "tr-123",
				FactKind:   FactTypeSuccessPattern,
				ValidFrom:  time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
				Confidence: 0.85,
				DecayRate:  0.01,
				TenantID:   "tenant-x",
			},
			wantType: NodeTypeFact,
		},
		{
			name: "save and retrieve pattern",
			node: &Pattern{
				ID:           "pattern-store-1",
				Description:  "heavy bash usage pattern",
				Occurrences:  12,
				FirstSeen:    time.Date(2026, 1, 10, 8, 0, 0, 0, time.UTC),
				LastSeen:     time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
				RelatedFacts: []string{"fact-1", "fact-2"},
				Confidence:   0.7,
				DecayRate:    0.02,
				TenantID:     "tenant-x",
			},
			wantType: NodeTypePattern,
		},
		{
			name: "save and retrieve engine profile",
			node: &EngineProfile{
				ID:          "ep-store-1",
				EngineName:  "claude-code",
				SuccessRate: map[string]float64{"bug_fix": 0.9, "feature": 0.75},
				Strengths:   []string{"fast", "accurate"},
				Weaknesses:  []string{"costly"},
				Confidence:  0.8,
				DecayRate:   0.01,
				TenantID:    "tenant-x",
				ValidFrom:   time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
			},
			wantType: NodeTypeEngineProfile,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := newTestStore(t)
			ctx := context.Background()

			err := store.SaveNode(ctx, tt.node)
			require.NoError(t, err)

			nodes, err := store.ListNodes(ctx)
			require.NoError(t, err)
			require.Len(t, nodes, 1)

			assert.Equal(t, tt.node.NodeID(), nodes[0].NodeID())
			assert.Equal(t, tt.wantType, nodes[0].NodeType())
			assert.InDelta(t, tt.node.GetConfidence(), nodes[0].GetConfidence(), 0.001)
		})
	}
}

func TestSQLiteStore_SaveEdge(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()

	edge := Edge{
		FromID:    "a",
		ToID:      "b",
		Relation:  RelationRelatesTo,
		Weight:    0.8,
		CreatedAt: time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
	}

	err := store.SaveEdge(ctx, edge)
	require.NoError(t, err)

	// Upsert same edge with different weight.
	edge.Weight = 0.95
	err = store.SaveEdge(ctx, edge)
	require.NoError(t, err)
}

func TestSQLiteStore_DeleteNode(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()

	node := &Fact{
		ID:         "to-delete",
		Content:    "will be deleted",
		Confidence: 0.5,
		DecayRate:  0.01,
		ValidFrom:  time.Now(),
		TenantID:   "",
	}
	require.NoError(t, store.SaveNode(ctx, node))
	require.NoError(t, store.SaveEdge(ctx, Edge{
		FromID: "to-delete", ToID: "other", Relation: RelationRelatesTo, Weight: 1.0, CreatedAt: time.Now(),
	}))

	err := store.DeleteNode(ctx, "to-delete")
	require.NoError(t, err)

	nodes, err := store.ListNodes(ctx)
	require.NoError(t, err)
	assert.Empty(t, nodes)
}

func TestSQLiteStore_QueryNodes(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()

	// Add nodes for two tenants.
	require.NoError(t, store.SaveNode(ctx, &Fact{
		ID: "t1-f1", Content: "tenant 1 fact", Confidence: 0.9, DecayRate: 0.01,
		ValidFrom: time.Now(), TenantID: "tenant-1",
	}))
	require.NoError(t, store.SaveNode(ctx, &Fact{
		ID: "t2-f1", Content: "tenant 2 fact", Confidence: 0.8, DecayRate: 0.01,
		ValidFrom: time.Now(), TenantID: "tenant-2",
	}))
	require.NoError(t, store.SaveNode(ctx, &Fact{
		ID: "t1-f2", Content: "tenant 1 fact 2", Confidence: 0.7, DecayRate: 0.01,
		ValidFrom: time.Now(), TenantID: "tenant-1",
	}))

	tests := []struct {
		name      string
		tenantID  string
		wantCount int
	}{
		{name: "tenant-1 sees 2 nodes", tenantID: "tenant-1", wantCount: 2},
		{name: "tenant-2 sees 1 node", tenantID: "tenant-2", wantCount: 1},
		{name: "unknown tenant sees 0 nodes", tenantID: "tenant-3", wantCount: 0},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			nodes, err := store.QueryNodes(ctx, GraphQuery{TenantID: tt.tenantID})
			require.NoError(t, err)
			assert.Len(t, nodes, tt.wantCount)
		})
	}
}

func TestSQLiteStore_UpsertNode(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()

	node := &Fact{
		ID: "upsert-me", Content: "original", Confidence: 0.5,
		DecayRate: 0.01, ValidFrom: time.Now(), TenantID: "",
	}
	require.NoError(t, store.SaveNode(ctx, node))

	// Update confidence.
	node.Confidence = 0.9
	node.Content = "updated"
	require.NoError(t, store.SaveNode(ctx, node))

	nodes, err := store.ListNodes(ctx)
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	assert.InDelta(t, 0.9, nodes[0].GetConfidence(), 0.001)
}
