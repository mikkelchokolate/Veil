package api

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/audit"
)

func TestAuditPrimaryFailureWithDurableSpoolIsVisibleAsDegraded(t *testing.T) {
	root := t.TempDir()
	primary := filepath.Join(root, "primary-directory")
	if err := os.Mkdir(primary, 0o700); err != nil {
		t.Fatal(err)
	}
	recorder := audit.NewRecorder(primary, audit.RecorderOptions{SpoolPath: filepath.Join(root, "critical.spool")})
	state := &managementState{audit: recorder}
	if err := state.recordRequestAudit(nil, audit.Record{Action: "backup.restore.start", Success: true}); err != nil {
		t.Fatalf("durably spooled critical audit failed request: %v", err)
	}
	if !state.isAuditDegraded() || recorder.Degraded() == nil {
		t.Fatal("successful spool hid primary audit degradation")
	}
}

func TestProductionAuditRecorderSpoolsKeyRotationAndRestore(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	if err := os.WriteFile(statePath, []byte(`{"version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	auditPath := filepath.Join(filepath.Dir(statePath), "audit", "panel.jsonl")
	if err := os.MkdirAll(filepath.Dir(auditPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(auditPath, 0o700); err != nil {
		t.Fatal(err)
	}
	recorder := audit.NewRecorder(auditPath, audit.RecorderOptions{})
	state := &managementState{audit: recorder}
	if err := state.recordRequestAudit(nil, audit.Record{Action: "security.key.rotate", Success: true}); err != nil {
		t.Fatalf("production key rotation audit was dropped: %v", err)
	}
	if err := state.recordRequestAudit(nil, audit.Record{Action: "backup.restore", Success: true}); err != nil {
		t.Fatalf("production restore audit was dropped: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(auditPath), "critical.spool")); err != nil {
		t.Fatalf("production recorder did not enable the critical spool: %v", err)
	}
	if !state.isAuditDegraded() || recorder.Degraded() == nil {
		t.Fatal("successful spool hid primary audit degradation")
	}
}

func TestAuditSpoolReplayFailureRemainsVisible(t *testing.T) {
	root := t.TempDir()
	spool := filepath.Join(root, "critical.spool")
	if err := os.WriteFile(spool, []byte("not-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	recorder := audit.NewRecorder(filepath.Join(root, "audit.jsonl"), audit.RecorderOptions{SpoolPath: spool})
	if recorder.Degraded() == nil {
		t.Fatal("startup swallowed audit spool replay failure")
	}
	if _, err := os.Stat(spool); err != nil {
		t.Fatalf("failed spool was discarded: %v", err)
	}
}
