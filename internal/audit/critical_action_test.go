package audit

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCriticalAuditActionMatchesLivePanelActions(t *testing.T) {
	for _, action := range []string{
		"security.key.rotate",
		"setup.complete",
		"user.create",
		"user.update",
		"user.delete",
		"backup.create",
		"backup.prune",
		"backup.delete",
		"backup.verify",
		"backup.restore",
		"backup.restore.start",
		"auth.login",
		"auth.login.rate_limited",
		"auth.logout",
		"auth.session.revoke",
		"apply.rollback",
		"migrate_legacy",
		"set_credential",
		"rotate_credential",
		"issue_subscription_token",
		"revoke_subscription_token",
		"create_client",
		"update_settings",
		"delete_inbound",
		"service_restart",
	} {
		if !criticalAuditAction(action) {
			t.Errorf("criticalAuditAction(%q) = false", action)
		}
	}
	for _, action := range []string{"security.test", "client.list", "request.test"} {
		if criticalAuditAction(action) {
			t.Errorf("non-critical %s must not spool", action)
		}
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
	for _, action := range []string{"security.key.rotate", "backup.restore", "backup.create", "user.delete", "auth.login"} {
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
	if !recorder.SpoolDurable() {
		t.Fatal("recorder did not mark the accepted spool write durable")
	}
}

// TestSpoolDurableReflectsLastWrite (#981): a spool that has never accepted a
// write, or whose last write failed, must not report durable.
func TestSpoolDurableReflectsLastWrite(t *testing.T) {
	root := t.TempDir()
	// Primary is a directory so every primary append fails.
	primary := filepath.Join(root, "primary-dir")
	if err := os.Mkdir(primary, 0o700); err != nil {
		t.Fatal(err)
	}
	// Spool path is also a directory so spool writes fail too.
	spoolDir := filepath.Join(root, "spool-dir")
	if err := os.Mkdir(spoolDir, 0o700); err != nil {
		t.Fatal(err)
	}
	recorder := NewRecorder(primary, RecorderOptions{SpoolPath: spoolDir, BackpressurePolicy: "spool_critical"})
	if recorder.SpoolDurable() {
		t.Fatal("unwritten spool reported durable")
	}
	if err := recorder.Append(Record{Action: "backup.create", Success: true}); err == nil {
		t.Fatal("append with both sinks broken should fail")
	}
	if recorder.SpoolDurable() {
		t.Fatal("failed spool write reported durable")
	}
}
