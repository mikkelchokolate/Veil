package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrationHistoryChecksIteratorErrorBeforeSuccess(t *testing.T) {
	path := filepath.Join("migrate.go")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	source := string(body)
	loop := strings.Index(source, "for rows.Next()")
	closeRows := strings.Index(source[loop:], "if err := rows.Close()")
	iteratorErr := strings.Index(source[loop:], "if err := rows.Err()")
	if loop < 0 || closeRows < 0 {
		t.Fatal("migration history iterator structure not found")
	}
	if iteratorErr < 0 || iteratorErr > closeRows {
		t.Fatal("migration history can report success after an iterator failure; rows.Err must be checked before close/success")
	}
}
