package installer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendAuditEventWritesJSONLLine(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.jsonl")

	event := AuditEvent{
		Action:        "rollback.restore",
		BackupID:      "20240101_120000",
		Success:       true,
		Error:         "",
		RestoredFiles: []string{"/etc/foo.conf", "/etc/bar.conf"},
	}

	err := AppendAuditEvent(auditPath, event)
	if err != nil {
		t.Fatalf("AppendAuditEvent: %v", err)
	}

	// File should exist
	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit file: %v", err)
	}

	// Should be one line
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d: %s", len(lines), string(data))
	}

	// Parse as JSON
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(lines[0]), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// Check fields
	if parsed["action"] != "rollback.restore" {
		t.Fatalf("expected action 'rollback.restore', got %v", parsed["action"])
	}
	if parsed["backupID"] != "20240101_120000" {
		t.Fatalf("expected backupID '20240101_120000', got %v", parsed["backupID"])
	}
	if parsed["success"] != true {
		t.Fatalf("expected success=true, got %v", parsed["success"])
	}
	if parsed["timestamp"] == nil || parsed["timestamp"] == "" {
		t.Fatalf("expected non-empty timestamp")
	}
	rf, ok := parsed["restoredFiles"].([]interface{})
	if !ok || len(rf) != 2 {
		t.Fatalf("expected 2 restoredFiles, got %v", parsed["restoredFiles"])
	}

	// Check permissions 0600
	info, err := os.Stat(auditPath)
	if err != nil {
		t.Fatalf("stat audit file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600 permissions, got %o", info.Mode().Perm())
	}
}

func TestAppendAuditEventEmptyPathNoOp(t *testing.T) {
	event := AuditEvent{
		Action:   "rollback.restore",
		BackupID: "20240101_120000",
		Success:  true,
	}

	err := AppendAuditEvent("", event)
	if err != nil {
		t.Fatalf("AppendAuditEvent with empty path should be no-op, got: %v", err)
	}
}

func TestAppendAuditEventCreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "sub", "deep", "audit.jsonl")

	event := AuditEvent{
		Action:   "rollback.cleanup",
		BackupID: "20240101_120000",
		Success:  true,
	}

	err := AppendAuditEvent(auditPath, event)
	if err != nil {
		t.Fatalf("AppendAuditEvent: %v", err)
	}

	// File should exist
	if _, err := os.Stat(auditPath); err != nil {
		t.Fatalf("audit file should exist: %v", err)
	}
}

func TestAppendAuditEventAppendsMultipleLines(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.jsonl")

	event1 := AuditEvent{Action: "rollback.restore", BackupID: "id1", Success: true}
	event2 := AuditEvent{Action: "rollback.cleanup", BackupID: "id2", Success: false, Error: "something went wrong"}

	if err := AppendAuditEvent(auditPath, event1); err != nil {
		t.Fatalf("first append: %v", err)
	}
	if err := AppendAuditEvent(auditPath, event2); err != nil {
		t.Fatalf("second append: %v", err)
	}

	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit file: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %s", len(lines), string(data))
	}
}

func TestAppendAuditEventFailureEvent(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.jsonl")

	event := AuditEvent{
		Action:   "rollback.restore",
		BackupID: "20240101_120000",
		Success:  false,
		Error:    "backup not found",
	}

	err := AppendAuditEvent(auditPath, event)
	if err != nil {
		t.Fatalf("AppendAuditEvent: %v", err)
	}

	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit file: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if parsed["success"] != false {
		t.Fatalf("expected success=false, got %v", parsed["success"])
	}
	if parsed["error"] != "backup not found" {
		t.Fatalf("expected error='backup not found', got %v", parsed["error"])
	}
}
