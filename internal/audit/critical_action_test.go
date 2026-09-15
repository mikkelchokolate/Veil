package audit

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCriticalAuditActionMatchesLivePanelActions(t *testing.T) {
	for _, action := range []string{"security.key.rotate", "setup.complete", "user.update", "backup.restore", "backup.restore.start"} {
		if !criticalAuditAction(action) {
			t.Errorf("criticalAuditAction(%q) = false", action)
		}
	}
	if criticalAuditAction("security.test") {
		t.Fatal("non-critical security.test must not spool")
	}
}

func TestProductionAuditPathLayoutSpoolsCriticalEventsWithoutOptions(t *testing.T) {
	root := t.TempDir()
	auditDir := filepath.Join(root, "audit")
	if err := os.MkdirAll(auditDir, 0o700); err != nil {
		t.Fatal(err)
	}
	primary := filepath.Join(auditDir, "panel.jsonl")
	if err := os.Mkdir(primary, 0o700); err != nil {
		t.Fatal(err)
	}
	recorder := NewRecorder(primary, RecorderOptions{})
	for _, action := range []string{"security.key.rotate", "backup.restore"} {
		if err := recorder.Append(Record{Actor: "admin", Action: action, Success: true}); err != nil {
			t.Fatalf("critical %s was dropped instead of spooled: %v", action, err)
		}
	}
	info, err := os.Stat(filepath.Join(auditDir, "critical.spool"))
	if err != nil {
		t.Fatalf("production spool missing: %v", err)
	}
	if info.Size() <= 0 {
		t.Fatal("production spool was not written")
	}
}
