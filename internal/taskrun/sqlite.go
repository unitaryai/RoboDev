package taskrun

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	_ "modernc.org/sqlite" // Pure Go SQLite driver.
)

const createTaskRunsTable = `CREATE TABLE IF NOT EXISTS task_runs (
	id TEXT PRIMARY KEY,
	idempotency_key TEXT NOT NULL,
	ticket_id TEXT NOT NULL DEFAULT '',
	state TEXT NOT NULL,
	job_name TEXT NOT NULL DEFAULT '',
	content_json TEXT NOT NULL,
	created_at DATETIME NOT NULL,
	updated_at DATETIME NOT NULL
)`

const createTaskRunsIdempotencyIdx = `CREATE INDEX IF NOT EXISTS idx_task_runs_idempotency_key ON task_runs(idempotency_key)`

const createTaskRunsTicketIdx = `CREATE INDEX IF NOT EXISTS idx_task_runs_ticket_id ON task_runs(ticket_id)`

const createTaskRunsStateIdx = `CREATE INDEX IF NOT EXISTS idx_task_runs_state ON task_runs(state)`

// SQLiteStore is a TaskRunStore backed by a SQLite database using the
// pure-Go modernc.org/sqlite driver (no CGO required). The full TaskRun is
// marshalled as JSON into the content_json column, which is the source of
// truth on read; the other columns are promoted copies of a subset of
// fields used purely as query keys.
type SQLiteStore struct {
	db     *sql.DB
	logger *slog.Logger
}

// NewSQLiteStore opens (or creates) a SQLite database at the given path and
// runs auto-migration to ensure the task_runs table exists. Use ":memory:"
// for an in-memory database (useful for tests).
func NewSQLiteStore(path string, logger *slog.Logger) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite database: %w", err)
	}

	// SQLite allows only one writer at a time. database/sql's connection
	// pool would otherwise hand concurrent callers separate connections
	// that contend for the single write lock and fail fast with
	// SQLITE_BUSY, so every operation is serialised through one connection.
	db.SetMaxOpenConns(1)

	// Enable WAL mode for better concurrent read performance.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("setting wal mode: %w", err)
	}

	// Give concurrent callers a busy timeout so they queue and retry on
	// SQLITE_BUSY rather than failing immediately.
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("setting busy timeout: %w", err)
	}

	store := &SQLiteStore{db: db, logger: logger}
	if err := store.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return store, nil
}

// migrate creates the task_runs table and indices if they do not already exist.
func (s *SQLiteStore) migrate() error {
	stmts := []string{
		createTaskRunsTable,
		createTaskRunsIdempotencyIdx,
		createTaskRunsTicketIdx,
		createTaskRunsStateIdx,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("executing migration: %w", err)
		}
	}
	return nil
}

// Save persists a TaskRun by upserting its row, keyed by ID.
func (s *SQLiteStore) Save(ctx context.Context, tr *TaskRun) error {
	if tr == nil {
		return fmt.Errorf("cannot save nil task run")
	}

	contentJSON, err := json.Marshal(tr)
	if err != nil {
		return fmt.Errorf("marshalling task run %q: %w", tr.ID, err)
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO task_runs (id, idempotency_key, ticket_id, state, job_name, content_json, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
			idempotency_key = excluded.idempotency_key,
			ticket_id = excluded.ticket_id,
			state = excluded.state,
			job_name = excluded.job_name,
			content_json = excluded.content_json,
			updated_at = excluded.updated_at`,
		tr.ID,
		tr.IdempotencyKey,
		tr.TicketID,
		string(tr.State),
		tr.JobName,
		string(contentJSON),
		tr.CreatedAt.UTC().Format(time.RFC3339),
		tr.UpdatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("upserting task run %q: %w", tr.ID, err)
	}
	return nil
}

// Get retrieves a TaskRun by ID. Returns an error if not found.
func (s *SQLiteStore) Get(ctx context.Context, id string) (*TaskRun, error) {
	var contentJSON string
	err := s.db.QueryRowContext(ctx, `SELECT content_json FROM task_runs WHERE id = ?`, id).Scan(&contentJSON)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("task run %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("querying task run %q: %w", id, err)
	}

	var tr TaskRun
	if err := json.Unmarshal([]byte(contentJSON), &tr); err != nil {
		return nil, fmt.Errorf("unmarshalling task run %q: %w", id, err)
	}
	return &tr, nil
}

// List returns all stored TaskRuns in no guaranteed order.
func (s *SQLiteStore) List(ctx context.Context) ([]*TaskRun, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT content_json FROM task_runs`)
	if err != nil {
		return nil, fmt.Errorf("listing task runs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return s.scanTaskRuns(rows)
}

// ListByTicketID returns all TaskRuns with the given ticket ID.
func (s *SQLiteStore) ListByTicketID(ctx context.Context, ticketID string) ([]*TaskRun, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT content_json FROM task_runs WHERE ticket_id = ?`, ticketID)
	if err != nil {
		return nil, fmt.Errorf("listing task runs for ticket %q: %w", ticketID, err)
	}
	defer func() { _ = rows.Close() }()

	return s.scanTaskRuns(rows)
}

// Close closes the underlying database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// scanTaskRuns reads rows from a query and deserialises them into TaskRun values.
func (s *SQLiteStore) scanTaskRuns(rows *sql.Rows) ([]*TaskRun, error) {
	var result []*TaskRun
	for rows.Next() {
		var contentJSON string
		if err := rows.Scan(&contentJSON); err != nil {
			return nil, fmt.Errorf("scanning task run row: %w", err)
		}

		var tr TaskRun
		if err := json.Unmarshal([]byte(contentJSON), &tr); err != nil {
			s.logger.Warn("skipping undeserialisable task run", "error", err)
			continue
		}
		result = append(result, &tr)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating task run rows: %w", err)
	}
	return result, nil
}
