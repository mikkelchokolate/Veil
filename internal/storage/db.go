// Package storage provides the normalized SQLite persistence layer for Veil's
// relational domain data (revisions, apply jobs, clients, bindings,
// credentials, subscription tokens, and traffic telemetry). The encrypted
// Management snapshot remains the store for global settings; this package
// holds the data that benefits from relational constraints and querying.
package storage

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// OpenExisting opens an existing SQLite database without creating directories,
// changing journal mode, or applying migrations. It is intended for backup,
// restore-boundary, and integrity operations that must not mutate schema.
func OpenExisting(path string) (*sql.DB, error) {
	if path == "" {
		return nil, fmt.Errorf("storage: database path is required")
	}
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("storage: stat database: %w", err)
	}
	dsn := sqliteFileDSN(path, true)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("storage: open existing database: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("storage: ping existing database: %w", err)
	}
	return db, nil
}

// Open opens (creating if necessary) the SQLite database at path and applies
// any pending schema migrations. The database is configured for a single-node
// embedded workload: WAL journaling, foreign-key enforcement, and a busy
// timeout so a brief writer/reader overlap does not fail immediately.
func Open(path string) (*sql.DB, error) {
	if path == "" {
		return nil, fmt.Errorf("storage: database path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("storage: create database dir: %w", err)
	}
	// modernc.org/sqlite honours PRAGMA via DSN query params.
	dsn := sqliteFileDSN(path, false)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("storage: open database: %w", err)
	}
	// A single connection keeps WAL + foreign_keys semantics simple and avoids
	// SQLITE_BUSY surprises for the embedded single-process workload.
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("storage: ping database: %w", err)
	}
	if err := Migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func sqliteFileDSN(path string, existingOnly bool) string {
	query := url.Values{}
	if existingOnly {
		query.Set("mode", "rw")
	}
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "foreign_keys(1)")
	if !existingOnly {
		query.Add("_pragma", "journal_mode(WAL)")
	}
	query.Add("_pragma", "synchronous(FULL)")
	return (&url.URL{Scheme: "file", Path: path, RawQuery: query.Encode()}).String()
}
