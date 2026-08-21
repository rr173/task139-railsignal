// Package store is the SQLite persistence layer for the railway signal
// engine. It owns the schema, a single shared connection (SetMaxOpenConns(1))
// to serialise writes, and exposes query methods in two flavours:
//
//   - public methods that operate on the pool connection (e.g. InsertNode),
//   - "Tx" variants that accept a DBTX so callers can compose several writes
//     (and the reads that must see them) inside one transaction.
//
// With SetMaxOpenConns(1), a transaction holds the only connection: any read
// inside a WithTx closure MUST go through the Tx variant, or it deadlocks
// waiting for a connection the transaction already holds.
package store

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// DBTX is the common surface of *sql.DB and *sql.Tx.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// Store wraps the SQLite database.
type Store struct {
	db *sql.DB
}

// schema is the DDL. CGO is disabled, so we use modernc.org/sqlite (file-based
// or in-memory ":memory:").
const schema = `
CREATE TABLE IF NOT EXISTS nodes (
    id   TEXT PRIMARY KEY,
    code TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sections (
    id              TEXT PRIMARY KEY,
    code            TEXT NOT NULL,
    length_m        INTEGER NOT NULL,
    kind            TEXT NOT NULL,
    from_node       TEXT NOT NULL,
    to_node         TEXT NOT NULL,
    occupancy_count INTEGER NOT NULL DEFAULT 0,
    locked_by_route TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS points (
    id                TEXT PRIMARY KEY,
    code              TEXT NOT NULL,
    node_id           TEXT NOT NULL,
    heel_section      TEXT NOT NULL,
    normal_section    TEXT NOT NULL,
    reverse_section   TEXT NOT NULL,
    direction         TEXT NOT NULL,
    status            TEXT NOT NULL,
    target_direction  TEXT NOT NULL DEFAULT '',
    move_start_time   INTEGER NOT NULL DEFAULT 0,
    move_deadline     INTEGER NOT NULL DEFAULT 0,
    max_move_seconds  INTEGER NOT NULL,
    locked_by_route   TEXT NOT NULL DEFAULT '',
    bypassed          INTEGER NOT NULL DEFAULT 0,
    protect_sections  TEXT NOT NULL DEFAULT '[]'
);

CREATE TABLE IF NOT EXISTS signals (
    id             TEXT PRIMARY KEY,
    code           TEXT NOT NULL,
    entry_node     TEXT NOT NULL,
    guard_section  TEXT NOT NULL,
    aspect         TEXT NOT NULL,
    status         TEXT NOT NULL,
    route_id       TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS routes (
    id                 TEXT PRIMARY KEY,
    code               TEXT NOT NULL,
    origin_signal      TEXT NOT NULL,
    terminal_section   TEXT NOT NULL,
    terminal_kind      TEXT NOT NULL,
    state              TEXT NOT NULL,
    path_sections      TEXT NOT NULL DEFAULT '[]',
    points_required    TEXT NOT NULL DEFAULT '[]',
    flank_protection   TEXT NOT NULL DEFAULT '[]',
    approach_section   TEXT NOT NULL DEFAULT '',
    opened_at          INTEGER NOT NULL DEFAULT 0,
    cancel_deadline    INTEGER NOT NULL DEFAULT 0,
    released_count     INTEGER NOT NULL DEFAULT 0,
    conflict_detail    TEXT NOT NULL DEFAULT '[]',
    diverging          INTEGER NOT NULL DEFAULT 0,
    transit_sec        INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS events (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    seq     INTEGER NOT NULL,
    kind    TEXT NOT NULL,
    payload TEXT NOT NULL DEFAULT '{}',
    clock   INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_events_seq ON events(seq);

CREATE TABLE IF NOT EXISTS meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_sections_locked ON sections(locked_by_route);
CREATE INDEX IF NOT EXISTS idx_points_locked ON points(locked_by_route);
CREATE INDEX IF NOT EXISTS idx_routes_state ON routes(state);
CREATE INDEX IF NOT EXISTS idx_signals_route ON signals(route_id);
`

// New opens (or creates) the database at path, applies the schema, and pins a
// single connection so writes are serialised.
func New(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}
	// Single connection: a transaction then holds the only connection, so
	// any in-tx read must use the Tx variant (see WithTx).
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	// pragmatic concurrency defaults for modernc/sqlite
	_, _ = db.Exec("PRAGMA journal_mode=WAL")
	_, _ = db.Exec("PRAGMA foreign_keys=ON")
	_, _ = db.Exec("PRAGMA busy_timeout=5000")
	return &Store{db: db}, nil
}

// Close releases the database.
func (s *Store) Close() error { return s.db.Close() }

// DB exposes the underlying pool (used by recovery/health checks).
func (s *Store) DB() *sql.DB { return s.db }

// WithTx runs fn inside a transaction on the pool connection. Because the pool
// has a single connection, fn MUST perform reads through the *sql.Tx (call the
// Tx-variant query methods), not through s.db, or the read will block forever.
func (s *Store) WithTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
