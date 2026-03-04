package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	_ "modernc.org/sqlite" // Pure Go SQLite driver.
)

// MemoryStore is the interface for persisting and querying knowledge graph
// nodes and edges. Implementations must be safe for concurrent use.
type MemoryStore interface {
	// SaveNode persists a node, creating or updating it by ID.
	SaveNode(ctx context.Context, node Node) error

	// SaveEdge persists an edge.
	SaveEdge(ctx context.Context, edge Edge) error

	// QueryNodes returns nodes matching the given query parameters.
	QueryNodes(ctx context.Context, query GraphQuery) ([]Node, error)

	// DeleteNode removes a node by ID. If tenantID is non-empty, an error is
	// returned if the node belongs to a different tenant.
	DeleteNode(ctx context.Context, id, tenantID string) error

	// ListNodes returns all stored nodes. If tenantID is non-empty, only nodes
	// belonging to that tenant are returned; an empty string returns all nodes
	// (for internal/administrative use).
	ListNodes(ctx context.Context, tenantID string) ([]Node, error)

	// Close releases any resources held by the store.
	Close() error
}

const createNodesTable = `CREATE TABLE IF NOT EXISTS nodes (
	id TEXT PRIMARY KEY,
	type TEXT NOT NULL,
	content_json TEXT NOT NULL,
	confidence REAL NOT NULL DEFAULT 1.0,
	decay_rate REAL NOT NULL DEFAULT 0.01,
	valid_from DATETIME NOT NULL,
	tenant_id TEXT NOT NULL DEFAULT ''
)`

const createEdgesTable = `CREATE TABLE IF NOT EXISTS edges (
	from_id TEXT NOT NULL,
	to_id TEXT NOT NULL,
	relation TEXT NOT NULL,
	weight REAL NOT NULL DEFAULT 1.0,
	created_at DATETIME NOT NULL,
	PRIMARY KEY (from_id, to_id, relation)
)`

const createNodesTenantIdx = `CREATE INDEX IF NOT EXISTS idx_nodes_tenant ON nodes(tenant_id)`
const createNodesTypeIdx = `CREATE INDEX IF NOT EXISTS idx_nodes_type ON nodes(type)`

// SQLiteStore is a MemoryStore backed by a SQLite database using the pure-Go
// modernc.org/sqlite driver (no CGO required).
type SQLiteStore struct {
	db     *sql.DB
	logger *slog.Logger
}

// NewSQLiteStore opens (or creates) a SQLite database at the given path and
// runs auto-migration to ensure tables exist. Use ":memory:" for an
// in-memory database (useful for tests).
func NewSQLiteStore(path string, logger *slog.Logger) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite database: %w", err)
	}

	// Enable WAL mode for better concurrent read performance.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("setting wal mode: %w", err)
	}

	store := &SQLiteStore{db: db, logger: logger}
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return store, nil
}

// migrate creates tables and indices if they do not already exist.
func (s *SQLiteStore) migrate() error {
	stmts := []string{
		createNodesTable,
		createEdgesTable,
		createNodesTenantIdx,
		createNodesTypeIdx,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("executing migration %q: %w", stmt[:40], err)
		}
	}
	return nil
}

// SaveNode persists a node by upserting its row.
func (s *SQLiteStore) SaveNode(ctx context.Context, node Node) error {
	contentJSON, err := json.Marshal(node)
	if err != nil {
		return fmt.Errorf("marshalling node %q: %w", node.NodeID(), err)
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO nodes (id, type, content_json, confidence, decay_rate, valid_from, tenant_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
			content_json = excluded.content_json,
			confidence = excluded.confidence,
			decay_rate = excluded.decay_rate`,
		node.NodeID(),
		string(node.NodeType()),
		string(contentJSON),
		node.GetConfidence(),
		node.GetDecayRate(),
		node.GetValidFrom().UTC().Format(time.RFC3339),
		node.GetTenantID(),
	)
	if err != nil {
		return fmt.Errorf("upserting node %q: %w", node.NodeID(), err)
	}
	return nil
}

// SaveEdge persists an edge by upserting its row. It validates that both
// endpoint nodes belong to the same tenant to prevent cross-tenant edges.
func (s *SQLiteStore) SaveEdge(ctx context.Context, edge Edge) error {
	// Validate tenant isolation: both nodes must share the same tenant.
	fromTenant, err := s.nodeTenantID(ctx, edge.FromID)
	if err != nil {
		return fmt.Errorf("looking up tenant for from_id %q: %w", edge.FromID, err)
	}
	toTenant, err := s.nodeTenantID(ctx, edge.ToID)
	if err != nil {
		return fmt.Errorf("looking up tenant for to_id %q: %w", edge.ToID, err)
	}
	if fromTenant != toTenant {
		return fmt.Errorf("cross-tenant edge rejected: %q (tenant %q) -> %q (tenant %q)",
			edge.FromID, fromTenant, edge.ToID, toTenant)
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO edges (from_id, to_id, relation, weight, created_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(from_id, to_id, relation) DO UPDATE SET
			weight = excluded.weight`,
		edge.FromID,
		edge.ToID,
		string(edge.Relation),
		edge.Weight,
		edge.CreatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("upserting edge %q->%q: %w", edge.FromID, edge.ToID, err)
	}
	return nil
}

// nodeTenantID retrieves the tenant_id for a node by its ID. If the node does
// not exist in the store (e.g. it is managed externally), an empty string is
// returned and no error is raised, effectively treating the node as untenanted.
func (s *SQLiteStore) nodeTenantID(ctx context.Context, nodeID string) (string, error) {
	var tenantID string
	err := s.db.QueryRowContext(ctx, `SELECT tenant_id FROM nodes WHERE id = ?`, nodeID).Scan(&tenantID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("querying tenant for node %q: %w", nodeID, err)
	}
	return tenantID, nil
}

// QueryNodes returns nodes filtered by tenant_id. Additional filtering
// (engine, description matching) is handled by the in-memory Graph layer.
func (s *SQLiteStore) QueryNodes(ctx context.Context, query GraphQuery) ([]Node, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT type, content_json FROM nodes WHERE tenant_id = ? ORDER BY confidence DESC`,
		query.TenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying nodes: %w", err)
	}
	defer rows.Close()

	return s.scanNodes(rows)
}

// DeleteNode removes a node and its associated edges. If tenantID is non-empty,
// it checks that the node belongs to that tenant before deleting; a mismatch
// returns an error without modifying the store.
func (s *SQLiteStore) DeleteNode(ctx context.Context, id, tenantID string) error {
	if tenantID != "" {
		nodeTenant, err := s.nodeTenantID(ctx, id)
		if err != nil {
			return fmt.Errorf("looking up node %q for deletion: %w", id, err)
		}
		if nodeTenant != tenantID {
			return fmt.Errorf("tenant mismatch: node %q belongs to tenant %q, not %q",
				id, nodeTenant, tenantID)
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx, `DELETE FROM edges WHERE from_id = ? OR to_id = ?`, id, id); err != nil {
		return fmt.Errorf("deleting edges for node %q: %w", id, err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM nodes WHERE id = ?`, id); err != nil {
		return fmt.Errorf("deleting node %q: %w", id, err)
	}
	return tx.Commit()
}

// ListNodes returns nodes from the store. If tenantID is non-empty, only nodes
// belonging to that tenant are returned; an empty string returns all nodes.
func (s *SQLiteStore) ListNodes(ctx context.Context, tenantID string) ([]Node, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if tenantID != "" {
		rows, err = s.db.QueryContext(ctx,
			`SELECT type, content_json FROM nodes WHERE tenant_id = ? ORDER BY confidence DESC`,
			tenantID,
		)
	} else {
		rows, err = s.db.QueryContext(ctx, `SELECT type, content_json FROM nodes ORDER BY confidence DESC`)
	}
	if err != nil {
		return nil, fmt.Errorf("listing nodes: %w", err)
	}
	defer rows.Close()

	return s.scanNodes(rows)
}

// Close closes the underlying database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// scanNodes reads rows from a query and deserialises them into Node values.
func (s *SQLiteStore) scanNodes(rows *sql.Rows) ([]Node, error) {
	var nodes []Node
	for rows.Next() {
		var nodeType string
		var contentJSON string
		if err := rows.Scan(&nodeType, &contentJSON); err != nil {
			return nil, fmt.Errorf("scanning node row: %w", err)
		}

		node, err := deserialiseNode(NodeType(nodeType), []byte(contentJSON))
		if err != nil {
			s.logger.Warn("skipping undeserialisable node",
				"type", nodeType,
				"error", err,
			)
			continue
		}
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating node rows: %w", err)
	}
	return nodes, nil
}

// deserialiseNode unmarshals JSON into the appropriate concrete Node type.
func deserialiseNode(nodeType NodeType, data []byte) (Node, error) {
	switch nodeType {
	case NodeTypeFact:
		var f Fact
		if err := json.Unmarshal(data, &f); err != nil {
			return nil, fmt.Errorf("unmarshalling fact: %w", err)
		}
		return &f, nil
	case NodeTypePattern:
		var p Pattern
		if err := json.Unmarshal(data, &p); err != nil {
			return nil, fmt.Errorf("unmarshalling pattern: %w", err)
		}
		return &p, nil
	case NodeTypeEngineProfile:
		var ep EngineProfile
		if err := json.Unmarshal(data, &ep); err != nil {
			return nil, fmt.Errorf("unmarshalling engine profile: %w", err)
		}
		return &ep, nil
	default:
		return nil, fmt.Errorf("unknown node type %q", nodeType)
	}
}
