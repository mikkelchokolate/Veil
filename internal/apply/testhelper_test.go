package apply

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/storage"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "veil.db"))
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	return db
}
