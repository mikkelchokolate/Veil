package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRecorderAppendRepairsTornTailBeforeWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panel.jsonl")
	complete := `{"timestamp":"2026-01-01T00:00:00Z","actor":"admin","action":"ok","success":true}` + "\n"
	if err := os.WriteFile(path, []byte(complete+`{"timestamp":"2026-01-01T00:00`), 0o600); err != nil {
		t.Fatal(err)
	}
	recorder := NewRecorder(path, RecorderOptions{})
	before, err := recorder.List(10, time.Time{})
	if err != nil {
		t.Fatalf("history should be readable before append: %v", err)
	}
	if len(before) != 1 || before[0].Action != "ok" {
		t.Fatalf("before=%+v", before)
	}
	if err := recorder.Append(Record{
		Timestamp: time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC),
		Actor:     "admin",
		Action:    "next",
		Success:   true,
	}); err != nil {
		t.Fatal(err)
	}
	after, err := recorder.List(10, time.Time{})
	if err != nil {
		t.Fatalf("history became unreadable after append: %v", err)
	}
	if len(after) != 2 {
		t.Fatalf("after=%+v", after)
	}
	actions := map[string]bool{after[0].Action: true, after[1].Action: true}
	if !actions["ok"] || !actions["next"] {
		t.Fatalf("records=%+v", after)
	}
}

func TestRecorderAppendAddsNewlineAfterCompleteRecordWithoutTerminator(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panel.jsonl")
	complete := `{"timestamp":"2026-01-01T00:00:00Z","actor":"admin","action":"ok","success":true}`
	if err := os.WriteFile(path, []byte(complete), 0o600); err != nil {
		t.Fatal(err)
	}
	recorder := NewRecorder(path, RecorderOptions{})
	if err := recorder.Append(Record{
		Timestamp: time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC),
		Actor:     "admin",
		Action:    "next",
		Success:   true,
	}); err != nil {
		t.Fatal(err)
	}
	records, err := recorder.List(10, time.Time{})
	if err != nil {
		t.Fatalf("complete record without newline should remain readable: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("records=%+v", records)
	}
}

func TestRecorderAppendDoesNotRewriteMiddleLineCorruption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panel.jsonl")
	body := `{"timestamp":"2026-01-01T00:00:00Z","actor":"admin","action":"first","success":true}` + "\n" +
		"not-json\n" +
		`{"timestamp":"2026-01-01T00:00:01Z","actor":"admin","action":"last","success":true}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	recorder := NewRecorder(path, RecorderOptions{})
	if err := recorder.Append(Record{Actor: "admin", Action: "next", Success: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := recorder.List(10, time.Time{}); err == nil {
		t.Fatal("middle-line corruption was ignored after append")
	}
}

func TestRecorderRepairsTornTailBeforeSpoolReplay(t *testing.T) {
	root := t.TempDir()
	primary := filepath.Join(root, "panel.jsonl")
	complete := `{"timestamp":"2026-01-01T00:00:00Z","actor":"admin","action":"ok","success":true}` + "\n"
	if err := os.WriteFile(primary, []byte(complete+`{"timestamp":"2026-01-01T00:00`), 0o600); err != nil {
		t.Fatal(err)
	}
	spool := filepath.Join(root, "critical.spool")
	spooled := `{"timestamp":"2026-01-01T00:00:02Z","actor":"admin","action":"backup.restore","success":true}` + "\n"
	if err := os.WriteFile(spool, []byte(spooled), 0o600); err != nil {
		t.Fatal(err)
	}
	recorder := NewRecorder(primary, RecorderOptions{
		SpoolPath: spool, BackpressurePolicy: "spool_critical",
	})
	if err := recorder.Degraded(); err != nil {
		t.Fatalf("spool replay after torn tail failed: %v", err)
	}
	records, err := recorder.List(10, time.Time{})
	if err != nil {
		t.Fatalf("history unreadable after spool replay: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("records=%+v", records)
	}
	body, err := os.ReadFile(primary)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(body), `{"timestamp"`) != 2 || strings.Contains(string(body), `{"timestamp":"2026-01-01T00:00{`) {
		t.Fatalf("torn tail was concatenated during spool replay: %s", body)
	}
}

func TestRecorderTornTailRepairIsDurableAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panel.jsonl")
	complete := `{"timestamp":"2026-01-01T00:00:00Z","actor":"admin","action":"ok","success":true}` + "\n"
	if err := os.WriteFile(path, []byte(complete+`{"timestamp":"2026-01-01T00:00`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := NewRecorder(path, RecorderOptions{}).Append(Record{
		Timestamp: time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC),
		Actor:     "admin",
		Action:    "next",
		Success:   true,
	}); err != nil {
		t.Fatal(err)
	}
	records, err := NewRecorder(path, RecorderOptions{}).List(10, time.Time{})
	if err != nil {
		t.Fatalf("reopened history unreadable: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("records=%+v", records)
	}
}
