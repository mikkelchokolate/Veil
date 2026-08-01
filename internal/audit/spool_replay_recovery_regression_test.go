package audit

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecorderReplaysDurableSpoolWhenPrimaryRecovers(t *testing.T) {
	root := t.TempDir()
	primary := filepath.Join(root, "audit.jsonl")
	if err := os.Mkdir(primary, 0o700); err != nil {
		t.Fatal(err)
	}
	recorder := NewRecorder(primary, RecorderOptions{
		SpoolPath: filepath.Join(root, "critical.spool"), BackpressurePolicy: "spool_critical",
	})
	if err := recorder.Append(Record{Action: "backup.restore", Actor: "admin", Success: true}); err != nil {
		t.Fatal(err)
	}
	if recorder.Degraded() == nil {
		t.Fatal("successful durable spool hid primary degradation")
	}
	if err := os.Remove(primary); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Append(Record{Action: "client.list", Actor: "admin", Success: true}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Degraded(); err != nil {
		t.Fatalf("recorder remained degraded after spool replay: %v", err)
	}
	records, err := recorder.List(10, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("records after spool replay=%d, want 2", len(records))
	}
}
