package estimator

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"time"

	_ "modernc.org/sqlite" // Pure Go SQLite driver.
)

const createOutcomesTable = `CREATE TABLE IF NOT EXISTS prediction_outcomes (
	task_run_id TEXT PRIMARY KEY,
	engine TEXT NOT NULL,
	complexity_json TEXT NOT NULL,
	actual_cost REAL,
	actual_duration_ns INTEGER,
	success INTEGER,
	recorded_at DATETIME
)`

const createOutcomesEngineIdx = `CREATE INDEX IF NOT EXISTS idx_outcomes_engine ON prediction_outcomes(engine)`

// SQLiteEstimatorStore is an EstimatorStore backed by a SQLite database
// using the pure-Go modernc.org/sqlite driver (no CGO required).
type SQLiteEstimatorStore struct {
	db     *sql.DB
	logger *slog.Logger
}

// NewSQLiteEstimatorStore opens (or creates) a SQLite database at the
// given path and runs auto-migration to ensure the prediction_outcomes
// table exists.
func NewSQLiteEstimatorStore(path string, logger *slog.Logger) (*SQLiteEstimatorStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite database: %w", err)
	}

	// Enable WAL mode for better concurrent read performance.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("setting wal mode: %w", err)
	}

	store := &SQLiteEstimatorStore{db: db, logger: logger}
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return store, nil
}

// migrate creates the prediction_outcomes table and indices if they do
// not already exist.
func (s *SQLiteEstimatorStore) migrate() error {
	stmts := []string{createOutcomesTable, createOutcomesEngineIdx}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("executing migration: %w", err)
		}
	}
	return nil
}

// SaveOutcome persists a completed task outcome.
func (s *SQLiteEstimatorStore) SaveOutcome(ctx context.Context, outcome PredictionOutcome) error {
	if outcome.TaskRunID == "" {
		return fmt.Errorf("outcome must have a task run ID")
	}

	complexityJSON, err := json.Marshal(outcome.ComplexityScore)
	if err != nil {
		return fmt.Errorf("marshalling complexity score: %w", err)
	}

	successInt := 0
	if outcome.Success {
		successInt = 1
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO prediction_outcomes
		 (task_run_id, engine, complexity_json, actual_cost, actual_duration_ns, success, recorded_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		outcome.TaskRunID,
		outcome.Engine,
		string(complexityJSON),
		outcome.ActualCost,
		int64(outcome.ActualDuration),
		successInt,
		outcome.RecordedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("upserting outcome %q: %w", outcome.TaskRunID, err)
	}
	return nil
}

// QuerySimilar returns the k most similar historical outcomes based on
// Euclidean distance between complexity dimension vectors. If engine is
// non-empty only outcomes from that engine are considered.
func (s *SQLiteEstimatorStore) QuerySimilar(ctx context.Context, score ComplexityScore, engine string, k int) ([]PredictionOutcome, error) {
	var rows *sql.Rows
	var err error

	if engine != "" {
		rows, err = s.db.QueryContext(ctx,
			`SELECT task_run_id, engine, complexity_json, actual_cost, actual_duration_ns, success, recorded_at
			 FROM prediction_outcomes WHERE engine = ?`,
			engine,
		)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT task_run_id, engine, complexity_json, actual_cost, actual_duration_ns, success, recorded_at
			 FROM prediction_outcomes`,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("querying outcomes: %w", err)
	}
	defer rows.Close()

	type scored struct {
		outcome  PredictionOutcome
		distance float64
	}

	var candidates []scored
	for rows.Next() {
		var (
			o              PredictionOutcome
			complexityJSON string
			durationNs     int64
			successInt     int
			recordedAt     string
		)
		if err := rows.Scan(
			&o.TaskRunID,
			&o.Engine,
			&complexityJSON,
			&o.ActualCost,
			&durationNs,
			&successInt,
			&recordedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning outcome row: %w", err)
		}

		if err := json.Unmarshal([]byte(complexityJSON), &o.ComplexityScore); err != nil {
			s.logger.Warn("skipping undeserialisable outcome", "task_run_id", o.TaskRunID, "error", err)
			continue
		}

		o.ActualDuration = time.Duration(durationNs)
		o.Success = successInt != 0
		if t, err := time.Parse(time.RFC3339, recordedAt); err == nil {
			o.RecordedAt = t
		}

		d := euclideanDistance(score, o.ComplexityScore)
		candidates = append(candidates, scored{outcome: o, distance: d})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating outcome rows: %w", err)
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].distance < candidates[j].distance
	})

	k = min(k, len(candidates))
	result := make([]PredictionOutcome, k)
	for i := 0; i < k; i++ {
		result[i] = candidates[i].outcome
	}
	return result, nil
}

// Close closes the underlying database connection.
func (s *SQLiteEstimatorStore) Close() error {
	return s.db.Close()
}
