package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// spoolLine encodes one record exactly as the spool stores it: one JSON
// object per line.
func spoolLine(t *testing.T, record Record) []byte {
	t.Helper()
	if record.Timestamp.IsZero() {
		record.Timestamp = time.Now().UTC()
	}
	body, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	return append(body, '\n')
}

// TestCorruptSpoolLineQuarantinedAndReplayContinues (#1033): one undecodable
// line must not abort replay forever. The surviving records reach the primary,
// the spool drains, and the corrupt line is preserved in a sibling quarantine
// file — audit evidence is never silently discarded.
func TestCorruptSpoolLineQuarantinedAndReplayContinues(t *testing.T) {
	root := t.TempDir()
	primary := filepath.Join(root, "panel.jsonl")
	spool := filepath.Join(root, "critical.spool")

	body := append([]byte{}, spoolLine(t, Record{Actor: "admin", Action: "backup.create", Success: true})...)
	body = append(body, []byte(`{"action":"user.update","timestamp":"not-a-time",`)...)
	body = append(body, '\n')
	body = append(body, spoolLine(t, Record{Actor: "admin", Action: "user.delete", Success: true})...)
	if err := os.WriteFile(spool, body, 0o600); err != nil {
		t.Fatal(err)
	}

	// NewRecorder replays the spool immediately.
	recorder := NewRecorder(primary, RecorderOptions{SpoolPath: spool})
	if err := recorder.Degraded(); err != nil {
		t.Fatalf("replay stayed degraded: %v", err)
	}
	if _, err := os.Stat(spool); !os.IsNotExist(err) {
		t.Fatalf("spool still present after replay: %v", err)
	}
	records, err := recorder.List(10, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("replayed records=%d, want 2 (corrupt line must not abort replay)", len(records))
	}
	quarantine, err := os.ReadFile(spool + ".corrupt")
	if err != nil {
		t.Fatalf("corrupt line not quarantined: %v", err)
	}
	if !strings.Contains(string(quarantine), "not-a-time") {
		t.Fatalf("quarantine file lost the corrupt evidence: %q", quarantine)
	}
}

// TestPartialSpoolReplayDoesNotDuplicateRecords (#1033): when the primary
// fails mid-replay, the already-replayed prefix must leave the spool.
// Retrying later must not rewrite those records as duplicates — the old
// behavior re-appended the whole spool on every subsequent Append.
//
// The mid-replay failure is produced without fault injection: MaxBytes is
// sized so the second record's append rotates the primary, and the rotation
// target (panel.jsonl.1) is a directory — Remove fails BEFORE any bytes are
// written, so no half-written record exists to reconcile.
func TestPartialSpoolReplayDoesNotDuplicateRecords(t *testing.T) {
	root := t.TempDir()
	primary := filepath.Join(root, "panel.jsonl")
	spool := filepath.Join(root, "critical.spool")

	lines := [][]byte{
		spoolLine(t, Record{Actor: "admin", Action: "backup.create", Success: true}),
		spoolLine(t, Record{Actor: "admin", Action: "user.delete", Success: true}),
		spoolLine(t, Record{Actor: "admin", Action: "key.rotate", Success: true}),
	}
	var body []byte
	for _, line := range lines {
		body = append(body, line...)
	}
	if err := os.WriteFile(spool, body, 0o600); err != nil {
		t.Fatal(err)
	}

	// Occupying the first rotation slot with a NON-EMPTY directory makes the
	// rotation — and therefore the second record's append — fail cleanly
	// (os.Remove only refuses non-empty directories; the failure happens
	// before any bytes are written, so no half-written record exists).
	if err := os.MkdirAll(primary+".1", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(primary+".1", "occupant"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	recorder := NewRecorder(primary, RecorderOptions{
		SpoolPath: spool,
		// The first record fits the empty primary; appending a second exceeds
		// the cap and forces a rotation that cannot succeed.
		MaxBytes: int64(len(lines[0]) + len(lines[1]) - 1),
		Backups:  1,
	})
	if recorder.Degraded() == nil {
		t.Fatal("recorder should report degraded after a failed replay")
	}

	// The spool must now hold ONLY the un-replayed tail.
	tail, err := os.ReadFile(spool)
	if err != nil {
		t.Fatalf("spool missing after partial replay: %v", err)
	}
	if strings.Contains(string(tail), "backup.create") {
		t.Fatalf("already-replayed record retained in spool — retry would duplicate it: %q", tail)
	}
	if !strings.Contains(string(tail), "user.delete") || !strings.Contains(string(tail), "key.rotate") {
		t.Fatalf("un-replayed tail lost records: %q", tail)
	}

	// Primary recovered: drop the rotation blocker and lift the size cap so
	// the remaining replay appends fit without rotating history away.
	if err := os.RemoveAll(primary + ".1"); err != nil {
		t.Fatal(err)
	}
	recorder.mu.Lock()
	recorder.maxBytes = defaultRecorderMaxBytes
	recorder.mu.Unlock()
	if err := recorder.Append(Record{Actor: "admin", Action: "auth.login", Success: true}); err != nil {
		t.Fatalf("append after recovery: %v", err)
	}
	records, err := recorder.List(10, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	for _, record := range records {
		seen[record.Action]++
	}
	for _, action := range []string{"backup.create", "user.delete", "key.rotate", "auth.login"} {
		if seen[action] != 1 {
			t.Fatalf("action %s recorded %d times, want exactly 1 (no replay duplicates)", action, seen[action])
		}
	}
}
