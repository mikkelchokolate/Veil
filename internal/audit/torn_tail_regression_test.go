package audit

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecorderRecoversOnlyTornFinalLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	complete := `{"timestamp":"2026-01-01T00:00:00Z","actor":"admin","action":"ok","success":true}` + "\n"
	if err := os.WriteFile(path, []byte(complete+`{"timestamp":"2026-01-01T00:00`), 0o600); err != nil {
		t.Fatal(err)
	}
	records, err := NewRecorder(path, RecorderOptions{}).List(10, time.Time{})
	if err != nil {
		t.Fatalf("torn final line should recover: %v", err)
	}
	if len(records) != 1 || records[0].Action != "ok" {
		t.Fatalf("records=%+v", records)
	}
}

func TestRecorderRejectsCorruptCompletedOrMiddleLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	body := `{"timestamp":"2026-01-01T00:00:00Z","actor":"admin","action":"first","success":true}` + "\n" +
		"not-json\n" +
		`{"timestamp":"2026-01-01T00:00:01Z","actor":"admin","action":"last","success":true}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewRecorder(path, RecorderOptions{}).List(10, time.Time{}); err == nil {
		t.Fatal("middle-line corruption was ignored")
	}
}
