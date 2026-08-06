// Package testdb supplies isolated SQLite clones for ordinary tests.
//
// The template is created once per test process through storage.Open, then
// closed and held as immutable bytes. Every caller receives a private copy and
// opens it through storage.OpenExisting, so production migrations/integrity
// checks remain covered by the storage package's own tests.
package testdb

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/storage"
)

type templateState struct {
	bytes       []byte
	fingerprint string
	noWAL       bool
}

var (
	once        sync.Once
	template    templateState
	templateErr error
)

func createTemplate() {
	root, err := os.MkdirTemp("", "veil-testdb-template-")
	if err != nil {
		templateErr = err
		return
	}
	defer os.RemoveAll(root)
	path := filepath.Join(root, "veil.db")
	db, err := storage.Open(path)
	if err != nil {
		templateErr = err
		return
	}
	if _, err = db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		_ = db.Close()
		templateErr = fmt.Errorf("checkpoint template: %w", err)
		return
	}
	fingerprint, err := schemaFingerprint(db)
	closeErr := db.Close()
	if err != nil {
		templateErr = err
		return
	}
	if closeErr != nil {
		templateErr = closeErr
		return
	}
	body, err := os.ReadFile(path)
	if err != nil {
		templateErr = err
		return
	}
	_, walErr := os.Stat(path + "-wal")
	_, shmErr := os.Stat(path + "-shm")
	template = templateState{
		bytes:       append([]byte(nil), body...),
		fingerprint: fingerprint,
		noWAL:       os.IsNotExist(walErr) && os.IsNotExist(shmErr),
	}
	if !template.noWAL {
		templateErr = fmt.Errorf("template has active SQLite WAL sidecars")
	}
}

func ensureTemplate() error {
	once.Do(createTemplate)
	return templateErr
}

func schemaFingerprint(db *sql.DB) (string, error) {
	rows, err := db.Query(`SELECT type, name, tbl_name, COALESCE(sql, '') FROM sqlite_master WHERE type IN ('table','index','trigger','view') ORDER BY type, name`)
	if err != nil {
		return "", fmt.Errorf("read schema fingerprint: %w", err)
	}
	defer rows.Close()
	var parts []string
	for rows.Next() {
		var typ, name, table, sqlText string
		if err := rows.Scan(&typ, &name, &table, &sqlText); err != nil {
			return "", err
		}
		parts = append(parts, strings.Join([]string{typ, name, table, sqlText}, "\x00"))
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x01")))
	return hex.EncodeToString(sum[:]), nil
}

// Open returns a fresh, writable clone of the current schema template.
func Open(t testing.TB) *sql.DB {
	t.Helper()
	_, db := OpenPath(t)
	return db
}

// OpenPath returns the clone path and its production-compatible connection.
func OpenPath(t testing.TB) (string, *sql.DB) {
	t.Helper()
	if err := ensureTemplate(); err != nil {
		t.Fatalf("create test database template: %v", err)
	}
	root := t.TempDir()
	path := filepath.Join(root, "veil.db")
	if err := os.WriteFile(path, template.bytes, 0o600); err != nil {
		t.Fatalf("copy test database template: %v", err)
	}
	db, err := storage.OpenExisting(path)
	if err != nil {
		t.Fatalf("open test database clone: %v", err)
	}
	fingerprint, err := schemaFingerprint(db)
	if err != nil {
		_ = db.Close()
		t.Fatalf("fingerprint test database clone: %v", err)
	}
	if fingerprint != template.fingerprint {
		_ = db.Close()
		t.Fatalf("test database schema fingerprint mismatch: got %s want %s", fingerprint, template.fingerprint)
	}
	t.Cleanup(func() { _ = db.Close() })
	return path, db
}

// CloneTo writes a template clone at an explicit path for tests that exercise
// APIs which infer the database path from state.json or another fixture path.
func CloneTo(t testing.TB, path string) *sql.DB {
	t.Helper()
	if err := ensureTemplate(); err != nil {
		t.Fatalf("create test database template: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create test database clone directory: %v", err)
	}
	if err := os.WriteFile(path, template.bytes, 0o600); err != nil {
		t.Fatalf("write test database clone: %v", err)
	}
	db, err := storage.OpenExisting(path)
	if err != nil {
		t.Fatalf("open test database clone: %v", err)
	}
	fingerprint, err := schemaFingerprint(db)
	if err != nil || fingerprint != template.fingerprint {
		_ = db.Close()
		t.Fatalf("test database schema fingerprint mismatch: got %s want %s err=%v", fingerprint, template.fingerprint, err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// CloneIfMissing is used by package-level test state constructors. Existing
// files intentionally take the production Open path so migration/corruption
// tests keep their real database semantics.
func CloneIfMissing(path string) (*sql.DB, error) {
	if _, err := os.Stat(path); err == nil {
		return storage.OpenExisting(path)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if err := ensureTemplate(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, template.bytes, 0o600); err != nil {
		return nil, err
	}
	db, err := storage.OpenExisting(path)
	if err != nil {
		return nil, err
	}
	fingerprint, err := schemaFingerprint(db)
	if err != nil || fingerprint != template.fingerprint {
		_ = db.Close()
		return nil, fmt.Errorf("test database schema fingerprint mismatch: got %s want %s err=%v", fingerprint, template.fingerprint, err)
	}
	return db, nil
}

// TemplateInfo exposes immutable creation facts for the helper's own tests.
func TemplateInfo() (fingerprint string, hasActiveWAL bool, err error) {
	if err := ensureTemplate(); err != nil {
		return "", false, err
	}
	return template.fingerprint, !template.noWAL, nil
}

// SortedSchemaNames is useful to test callers that want a stable diagnostic.
func SortedSchemaNames(db *sql.DB) ([]string, error) {
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Strings(names)
	return names, nil
}
