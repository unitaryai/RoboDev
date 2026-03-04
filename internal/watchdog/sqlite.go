package watchdog

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	_ "modernc.org/sqlite" // Pure Go SQLite driver.
)

const createCalibratedProfilesTable = `CREATE TABLE IF NOT EXISTS calibrated_profiles (
	repo_pattern TEXT NOT NULL,
	engine TEXT NOT NULL,
	task_type TEXT NOT NULL,
	data_json TEXT NOT NULL,
	updated_at DATETIME NOT NULL,
	PRIMARY KEY (repo_pattern, engine, task_type)
)`

// SQLiteProfileStore is a ProfileStore backed by a SQLite database using
// the pure-Go modernc.org/sqlite driver (no CGO required).
type SQLiteProfileStore struct {
	db     *sql.DB
	logger *slog.Logger
}

// NewSQLiteProfileStore opens (or creates) a SQLite database at the
// given path and runs auto-migration to ensure the calibrated_profiles
// table exists.
func NewSQLiteProfileStore(path string, logger *slog.Logger) (*SQLiteProfileStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite database: %w", err)
	}

	// Enable WAL mode for better concurrent read performance.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("setting wal mode: %w", err)
	}

	store := &SQLiteProfileStore{db: db, logger: logger}
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return store, nil
}

// migrate creates the calibrated_profiles table if it does not already
// exist.
func (s *SQLiteProfileStore) migrate() error {
	if _, err := s.db.Exec(createCalibratedProfilesTable); err != nil {
		return fmt.Errorf("executing migration: %w", err)
	}
	return nil
}

// Get retrieves the calibrated profile for the exact key, or nil if no
// profile exists.
func (s *SQLiteProfileStore) Get(ctx context.Context, key ProfileKey) *CalibratedProfile {
	var dataJSON string
	err := s.db.QueryRowContext(ctx,
		`SELECT data_json FROM calibrated_profiles
		 WHERE repo_pattern = ? AND engine = ? AND task_type = ?`,
		key.RepoPattern, key.Engine, key.TaskType,
	).Scan(&dataJSON)
	if err != nil {
		if err != sql.ErrNoRows {
			s.logger.Error("reading calibrated profile", "key", key, "error", err)
		}
		return nil
	}

	var profile CalibratedProfile
	if err := json.Unmarshal([]byte(dataJSON), &profile); err != nil {
		s.logger.Error("unmarshalling calibrated profile", "key", key, "error", err)
		return nil
	}
	return &profile
}

// Put stores or updates a calibrated profile.
func (s *SQLiteProfileStore) Put(ctx context.Context, profile *CalibratedProfile) {
	if profile == nil {
		return
	}

	data, err := json.Marshal(profile)
	if err != nil {
		s.logger.Error("marshalling calibrated profile", "key", profile.Key, "error", err)
		return
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO calibrated_profiles
		 (repo_pattern, engine, task_type, data_json, updated_at)
		 VALUES (?, ?, ?, ?, ?)`,
		profile.Key.RepoPattern,
		profile.Key.Engine,
		profile.Key.TaskType,
		string(data),
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		s.logger.Error("upserting calibrated profile", "key", profile.Key, "error", err)
	}
}

// List returns all stored calibrated profiles.
func (s *SQLiteProfileStore) List(ctx context.Context) []*CalibratedProfile {
	rows, err := s.db.QueryContext(ctx, `SELECT data_json FROM calibrated_profiles`)
	if err != nil {
		s.logger.Error("listing calibrated profiles", "error", err)
		return nil
	}
	defer rows.Close()

	var result []*CalibratedProfile
	for rows.Next() {
		var dataJSON string
		if err := rows.Scan(&dataJSON); err != nil {
			s.logger.Error("scanning calibrated profile row", "error", err)
			continue
		}

		var profile CalibratedProfile
		if err := json.Unmarshal([]byte(dataJSON), &profile); err != nil {
			s.logger.Warn("skipping undeserialisable calibrated profile", "error", err)
			continue
		}
		result = append(result, &profile)
	}
	if err := rows.Err(); err != nil {
		s.logger.Error("iterating calibrated profile rows", "error", err)
	}
	return result
}

// Close closes the underlying database connection.
func (s *SQLiteProfileStore) Close() error {
	return s.db.Close()
}
