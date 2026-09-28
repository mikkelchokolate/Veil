package storage

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenExistingRequiresDatabaseAndDoesNotCreateIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "veil.db")
	if _, err := OpenExisting(path); err == nil {
		t.Fatal("OpenExisting created a missing database")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("missing database was created: %v", err)
	}
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	existing, err := OpenExisting(path)
	if err != nil {
		t.Fatal(err)
	}
	defer existing.Close()
	var version int
	if err := existing.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version == 0 {
		t.Fatal("existing database schema was not readable")
	}
}

// TestFileURIPathFromSlashDriveLetter is the #1145 regression: a Windows
// drive-letter path (C:/...) must gain a leading "/" so url.URL emits
// file:///C:/... rather than letting "C:" be parsed as a URI authority
// ("invalid uri authority" from modernc.org/sqlite). The helper is pure
// string logic, so the drive-letter contract is exercisable on any GOOS.
func TestFileURIPathFromSlashDriveLetter(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"C:/ProgramData/Veil/veil.db", "/C:/ProgramData/Veil/veil.db"},
		{"D:/x/y.db", "/D:/x/y.db"},
		{"/var/lib/veil/veil.db", "/var/lib/veil/veil.db"},
		{"//server/share/veil.db", "//server/share/veil.db"},
	}
	for _, tc := range cases {
		if got := fileURIPathFromSlash(tc.in); got != tc.want {
			t.Fatalf("fileURIPathFromSlash(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestSQLiteFileDSNHasNoAuthority asserts the emitted DSN never carries a URI
// authority: a path-derived authority is the Windows failure mode from
// #1145, and an empty authority keeps the path in the URI path component.
func TestSQLiteFileDSNHasNoAuthority(t *testing.T) {
	for _, p := range []string{filepath.Join(t.TempDir(), "veil.db"), "veil.db"} {
		dsn := sqliteFileDSN(p, false)
		if !strings.HasPrefix(dsn, "file:") {
			t.Fatalf("DSN for %q is not a file: URI: %q", p, dsn)
		}
		u, err := url.Parse(dsn)
		if err != nil {
			t.Fatalf("DSN for %q does not parse: %v (%q)", p, err, dsn)
		}
		if u.Host != "" {
			t.Fatalf("DSN for %q leaked a URI authority %q: %q", p, u.Host, dsn)
		}
		if !strings.HasSuffix(u.Path, "veil.db") {
			t.Fatalf("DSN for %q lost the file path: %q", p, dsn)
		}
	}
}

// TestOpenAbsolutePath exercises the real Open path against an absolute
// database path — the exact call that failed on Windows when the drive
// letter was parsed as a URI authority (issue #1145).
func TestOpenAbsolutePath(t *testing.T) {
	abs, err := filepath.Abs(filepath.Join(t.TempDir(), "veil.db"))
	if err != nil {
		t.Fatal(err)
	}
	db, err := Open(abs)
	if err != nil {
		t.Fatalf("Open absolute path: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}
