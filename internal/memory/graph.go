package memory

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"
)

// Graph is the in-memory temporal knowledge graph. It provides thread-safe
// access to nodes and edges, with temporal decay and confidence-based pruning.
type Graph struct {
	mu    sync.RWMutex
	nodes map[string]Node
	edges []Edge
	store MemoryStore

	logger *slog.Logger
}

// NewGraph creates a new Graph backed by the given MemoryStore. If store is
// nil the graph operates in memory-only mode (useful for tests).
func NewGraph(store MemoryStore, logger *slog.Logger) *Graph {
	return &Graph{
		nodes:  make(map[string]Node),
		edges:  make([]Edge, 0),
		store:  store,
		logger: logger,
	}
}

// AddNode inserts or updates a node in the graph and persists it to the
// backing store.
func (g *Graph) AddNode(ctx context.Context, node Node) error {
	if node == nil {
		return fmt.Errorf("cannot add nil node")
	}
	if node.NodeID() == "" {
		return fmt.Errorf("node must have a non-empty id")
	}

	g.mu.Lock()
	g.nodes[node.NodeID()] = node
	g.mu.Unlock()

	if g.store != nil {
		if err := g.store.SaveNode(ctx, node); err != nil {
			return fmt.Errorf("persisting node %q: %w", node.NodeID(), err)
		}
	}

	return nil
}

// AddEdge inserts an edge into the graph and persists it to the backing store.
func (g *Graph) AddEdge(ctx context.Context, edge Edge) error {
	if edge.FromID == "" || edge.ToID == "" {
		return fmt.Errorf("edge must have non-empty from_id and to_id")
	}

	g.mu.Lock()
	g.edges = append(g.edges, edge)
	g.mu.Unlock()

	if g.store != nil {
		if err := g.store.SaveEdge(ctx, edge); err != nil {
			return fmt.Errorf("persisting edge %q->%q: %w", edge.FromID, edge.ToID, err)
		}
	}

	return nil
}

// GetNode returns a node by ID, or nil if it does not exist.
func (g *Graph) GetNode(id string) Node {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.nodes[id]
}

// Query retrieves nodes matching the given query, scored by temporal
// weighting (confidence * recency). Results are sorted by descending
// score and limited to MaxResults.
func (g *Graph) Query(_ context.Context, query GraphQuery) ([]Node, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	type scored struct {
		node  Node
		score float64
	}

	var candidates []scored

	for _, node := range g.nodes {
		// Enforce tenant isolation.
		if query.TenantID != "" && node.GetTenantID() != query.TenantID {
			continue
		}

		// Compute temporal score: confidence decayed by age.
		age := time.Since(node.GetValidFrom()).Hours()
		// Recency multiplier: exponential decay over 720 hours (30 days).
		recency := 1.0
		if age > 0 {
			recency = 1.0 / (1.0 + age/720.0)
		}
		score := node.GetConfidence() * recency

		// Filter by engine if specified (only applies to EngineProfile nodes
		// and facts related to a specific engine).
		if query.Engine != "" {
			if ep, ok := node.(*EngineProfile); ok {
				if ep.EngineName != query.Engine {
					continue
				}
			}
		}

		candidates = append(candidates, scored{node: node, score: score})
	}

	// Sort by descending score.
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	maxResults := query.MaxResults
	if maxResults <= 0 {
		maxResults = 20
	}
	if len(candidates) > maxResults {
		candidates = candidates[:maxResults]
	}

	result := make([]Node, len(candidates))
	for i, c := range candidates {
		result[i] = c.node
	}

	return result, nil
}

// DecayConfidence applies temporal decay to all nodes in the graph.
// Each node's confidence is multiplied by (1 - decay_rate). Nodes whose
// confidence drops below a small epsilon are left for PruneStale to remove.
func (g *Graph) DecayConfidence(_ context.Context) {
	g.mu.Lock()
	defer g.mu.Unlock()

	for _, node := range g.nodes {
		current := node.GetConfidence()
		decayRate := node.GetDecayRate()
		if decayRate <= 0 {
			continue
		}
		newConfidence := current * (1.0 - decayRate)
		if newConfidence < 0 {
			newConfidence = 0
		}
		node.SetConfidence(newConfidence)
	}

	g.logger.Debug("applied confidence decay", "node_count", len(g.nodes))
}

// PruneStale removes all nodes whose confidence has fallen below the
// given threshold. Associated edges are also removed.
func (g *Graph) PruneStale(ctx context.Context, threshold float64) int {
	g.mu.Lock()

	var pruned []string
	for id, node := range g.nodes {
		if node.GetConfidence() < threshold {
			pruned = append(pruned, id)
			delete(g.nodes, id)
		}
	}

	// Remove edges referencing pruned nodes.
	if len(pruned) > 0 {
		prunedSet := make(map[string]struct{}, len(pruned))
		for _, id := range pruned {
			prunedSet[id] = struct{}{}
		}

		remaining := make([]Edge, 0, len(g.edges))
		for _, e := range g.edges {
			_, fromPruned := prunedSet[e.FromID]
			_, toPruned := prunedSet[e.ToID]
			if !fromPruned && !toPruned {
				remaining = append(remaining, e)
			}
		}
		g.edges = remaining
	}

	g.mu.Unlock()

	// Delete from backing store.
	if g.store != nil {
		for _, id := range pruned {
			if err := g.store.DeleteNode(ctx, id); err != nil {
				g.logger.Warn("failed to delete pruned node from store",
					"node_id", id,
					"error", err,
				)
			}
		}
	}

	if len(pruned) > 0 {
		g.logger.Info("pruned stale nodes",
			"count", len(pruned),
			"threshold", threshold,
		)
	}

	return len(pruned)
}

// NodeCount returns the number of nodes currently in the graph.
func (g *Graph) NodeCount() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.nodes)
}

// EdgeCount returns the number of edges currently in the graph.
func (g *Graph) EdgeCount() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.edges)
}

// LoadFromStore populates the in-memory graph from the backing store.
// This should be called once at startup.
func (g *Graph) LoadFromStore(ctx context.Context) error {
	if g.store == nil {
		return nil
	}

	nodes, err := g.store.ListNodes(ctx)
	if err != nil {
		return fmt.Errorf("loading nodes from store: %w", err)
	}

	g.mu.Lock()
	for _, node := range nodes {
		g.nodes[node.NodeID()] = node
	}
	g.mu.Unlock()

	g.logger.Info("loaded memory graph from store", "node_count", len(nodes))
	return nil
}
