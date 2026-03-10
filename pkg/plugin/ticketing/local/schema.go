package local

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // Pure Go SQLite driver.
)

const createTicketsTable = `CREATE TABLE IF NOT EXISTS tickets (
	id TEXT PRIMARY KEY,
	title TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	ticket_type TEXT NOT NULL DEFAULT '',
	labels_json TEXT NOT NULL DEFAULT '[]',
	repo_url TEXT NOT NULL DEFAULT '',
	external_url TEXT NOT NULL DEFAULT '',
	raw_json TEXT NOT NULL DEFAULT '{}',
	state TEXT NOT NULL,
	run_state TEXT NOT NULL DEFAULT 'idle',
	result_json TEXT NOT NULL DEFAULT '',
	summary TEXT NOT NULL DEFAULT '',
	branch_name TEXT NOT NULL DEFAULT '',
	merge_request_url TEXT NOT NULL DEFAULT '',
	input_tokens INTEGER NOT NULL DEFAULT 0,
	output_tokens INTEGER NOT NULL DEFAULT 0,
	cost_estimate_usd REAL NOT NULL DEFAULT 0,
	failure_reason TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	in_progress_at TEXT NOT NULL DEFAULT '',
	completed_at TEXT NOT NULL DEFAULT '',
	failed_at TEXT NOT NULL DEFAULT ''
)`

const createCommentsTable = `CREATE TABLE IF NOT EXISTS ticket_comments (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	ticket_id TEXT NOT NULL,
	kind TEXT NOT NULL,
	body TEXT NOT NULL,
	created_at TEXT NOT NULL
)`

const createEventsTable = `CREATE TABLE IF NOT EXISTS ticket_events (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	ticket_id TEXT NOT NULL,
	event_type TEXT NOT NULL,
	payload_json TEXT NOT NULL DEFAULT '{}',
	created_at TEXT NOT NULL
)`

const (
	createTicketsStateIdx   = `CREATE INDEX IF NOT EXISTS idx_tickets_state_created_at ON tickets(state, created_at, id)`
	createCommentsTicketIdx = `CREATE INDEX IF NOT EXISTS idx_ticket_comments_ticket_created_at ON ticket_comments(ticket_id, created_at, id)`
	createEventsTicketIdx   = `CREATE INDEX IF NOT EXISTS idx_ticket_events_ticket_created_at ON ticket_events(ticket_id, created_at, id)`
)

func openDatabase(path string) (*sql.DB, error) {
	if err := ensureStoreDirectory(path); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite database: %w", err)
	}
	if path == ":memory:" {
		db.SetMaxOpenConns(1)
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("setting wal mode: %w", err)
	}

	return db, nil
}

func ensureStoreDirectory(path string) error {
	if path == "" || path == ":memory:" {
		return nil
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("creating store directory %q: %w", dir, err)
	}

	return nil
}

func (b *Backend) migrate() error {
	statements := []string{
		createTicketsTable,
		createCommentsTable,
		createEventsTable,
		createCommentsTicketIdx,
		createEventsTicketIdx,
		createTicketsStateIdx,
	}

	for _, statement := range statements {
		if _, err := b.db.Exec(statement); err != nil {
			return fmt.Errorf("executing migration: %w", err)
		}
	}

	if err := ensureTicketColumn(b.db, "run_state", "TEXT NOT NULL DEFAULT 'idle'"); err != nil {
		return err
	}
	if err := migrateTrackerStates(b.db); err != nil {
		return err
	}

	return nil
}

func ensureTicketColumn(db *sql.DB, name, definition string) error {
	exists, err := ticketColumnExists(db, name)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	statement := fmt.Sprintf("ALTER TABLE tickets ADD COLUMN %s %s", name, definition)
	if _, err := db.Exec(statement); err != nil {
		return fmt.Errorf("adding tickets.%s column: %w", name, err)
	}

	return nil
}

func ticketColumnExists(db *sql.DB, name string) (bool, error) {
	rows, err := db.Query(`PRAGMA table_info(tickets)`)
	if err != nil {
		return false, fmt.Errorf("querying tickets table info: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			columnName string
			columnType string
			notNull    int
			defaultVal sql.NullString
			primaryKey int
		)
		if err := rows.Scan(&cid, &columnName, &columnType, &notNull, &defaultVal, &primaryKey); err != nil {
			return false, fmt.Errorf("scanning tickets table info: %w", err)
		}
		if columnName == name {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterating tickets table info: %w", err)
	}

	return false, nil
}

func migrateTrackerStates(db *sql.DB) error {
	statements := []string{
		`UPDATE tickets
		 SET run_state = CASE
			 WHEN run_state IN ('idle', 'running', 'succeeded', 'failed') THEN run_state
			 WHEN state = 'in_progress' THEN 'running'
			 WHEN state = 'completed' THEN 'succeeded'
			 WHEN state = 'failed' THEN 'failed'
			 ELSE 'idle'
		 END`,
		`UPDATE tickets
		 SET state = CASE
			 WHEN state = 'ready' THEN 'todo'
			 WHEN state = 'completed' THEN 'done'
			 WHEN state = 'failed' THEN 'in_progress'
			 ELSE state
		 END`,
	}

	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return fmt.Errorf("migrating tracker state model: %w", err)
		}
	}

	return nil
}
