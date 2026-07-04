package rollback

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/audit"
	"github.com/mikkelchokolate/Veil/internal/backup"
)

func TestWorkflowListsAndRestoresBackup(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")
	managed := filepath.Join(dir, "veil.env")
	if err := os.WriteFile(managed, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	backupID, err := backup.NewLifecycle(backupDir).BackupExisting([]string{managed})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(managed, []byte("after"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	workflow := NewWorkflow(Options{BackupDir: backupDir, Yes: true}, &out)
	if err := workflow.List(); err != nil {
		t.Fatalf("List: %v", err)
	}
	if !strings.Contains(out.String(), backupID) {
		t.Fatalf("list output missing backup ID: %s", out.String())
	}
	out.Reset()
	if err := workflow.Restore(backupID); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	body, err := os.ReadFile(managed)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "before" {
		t.Fatalf("restored body = %q", string(body))
	}
	if !strings.Contains(out.String(), "Restored files:") {
		t.Fatalf("restore output = %s", out.String())
	}
}

func TestWorkflowRequiresBackupDirAndConfirmation(t *testing.T) {
	workflow := NewWorkflow(Options{}, &bytes.Buffer{})
	if err := workflow.List(); err == nil || !strings.Contains(err.Error(), "--backup-dir is required") {
		t.Fatalf("List err = %v", err)
	}
	workflow = NewWorkflow(Options{BackupDir: t.TempDir()}, &bytes.Buffer{})
	if err := workflow.Restore("backup"); err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("Restore err = %v", err)
	}
	if err := workflow.Cleanup("backup"); err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("Cleanup err = %v", err)
	}
}

type mockLifecycle struct {
	listIDs      []string
	listErr      error
	restoreFiles []string
	restoreErr   error
	cleanupErr   error
	restoreCalls []string
	cleanupCalls []string
}

func (m *mockLifecycle) List() ([]string, error) {
	return m.listIDs, m.listErr
}

func (m *mockLifecycle) Restore(backupID string) ([]string, error) {
	m.restoreCalls = append(m.restoreCalls, backupID)
	return m.restoreFiles, m.restoreErr
}

func (m *mockLifecycle) Cleanup(backupID string) error {
	m.cleanupCalls = append(m.cleanupCalls, backupID)
	return m.cleanupErr
}

func TestWorkflowRestoreRequiresBackupDir(t *testing.T) {
	w := NewWorkflow(Options{Yes: true}, &bytes.Buffer{})
	if err := w.Restore("id"); err == nil || !strings.Contains(err.Error(), "--backup-dir is required") {
		t.Fatalf("Restore err = %v", err)
	}
}

func TestWorkflowCleanupRequiresBackupDir(t *testing.T) {
	w := NewWorkflow(Options{Yes: true}, &bytes.Buffer{})
	if err := w.Cleanup("id"); err == nil || !strings.Contains(err.Error(), "--backup-dir is required") {
		t.Fatalf("Cleanup err = %v", err)
	}
}

func TestWorkflowListEmpty(t *testing.T) {
	var out bytes.Buffer
	w := Workflow{
		opts: Options{BackupDir: "/backups"},
		out:  &out,
		lc:   &mockLifecycle{listIDs: []string{}},
	}
	if err := w.List(); err != nil {
		t.Fatalf("List: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "No backups found" {
		t.Fatalf("list output = %q", got)
	}
}

func TestWorkflowListError(t *testing.T) {
	w := Workflow{
		opts: Options{BackupDir: "/backups"},
		out:  &bytes.Buffer{},
		lc:   &mockLifecycle{listErr: errors.New("read failed")},
	}
	if err := w.List(); err == nil || !strings.Contains(err.Error(), "read failed") {
		t.Fatalf("List err = %v", err)
	}
}

func TestWorkflowListOutputsIDs(t *testing.T) {
	var out bytes.Buffer
	w := Workflow{
		opts: Options{BackupDir: "/backups"},
		out:  &out,
		lc:   &mockLifecycle{listIDs: []string{"id-1", "id-2"}},
	}
	if err := w.List(); err != nil {
		t.Fatalf("List: %v", err)
	}
	got := strings.TrimSpace(out.String())
	if !strings.Contains(got, "id-1") || !strings.Contains(got, "id-2") {
		t.Fatalf("list output = %q", got)
	}
}

func TestWorkflowRestoreErrorWritesAudit(t *testing.T) {
	oldAppend := appendAuditEvent
	t.Cleanup(func() { appendAuditEvent = oldAppend })

	var captured []audit.AuditEvent
	appendAuditEvent = func(path string, event audit.AuditEvent) error {
		captured = append(captured, event)
		return nil
	}

	w := Workflow{
		opts: Options{BackupDir: "/backups", Yes: true, AuditLog: "/tmp/audit.log"},
		out:  &bytes.Buffer{},
		lc:   &mockLifecycle{restoreErr: errors.New("restore failed")},
	}
	if err := w.Restore("bad-id"); err == nil || !strings.Contains(err.Error(), "restore failed") {
		t.Fatalf("Restore err = %v", err)
	}
	if len(captured) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(captured))
	}
	ev := captured[0]
	if ev.Action != "rollback.restore" || ev.Success || ev.Error != "restore failed" || ev.BackupID != "bad-id" {
		t.Fatalf("unexpected audit event: %+v", ev)
	}
}

func TestWorkflowRestoreAuditWriteFailure(t *testing.T) {
	oldAppend := appendAuditEvent
	t.Cleanup(func() { appendAuditEvent = oldAppend })

	appendAuditEvent = func(path string, event audit.AuditEvent) error {
		if event.Success {
			return errors.New("audit write failed")
		}
		return nil
	}

	w := Workflow{
		opts: Options{BackupDir: "/backups", Yes: true, AuditLog: "/tmp/audit.log"},
		out:  &bytes.Buffer{},
		lc:   &mockLifecycle{restoreFiles: []string{"/a", "/b"}},
	}
	err := w.Restore("id")
	if err == nil || !strings.Contains(err.Error(), "audit log write failed after successful restore") {
		t.Fatalf("Restore err = %v", err)
	}
}

func TestWorkflowCleanupSuccess(t *testing.T) {
	oldAppend := appendAuditEvent
	t.Cleanup(func() { appendAuditEvent = oldAppend })

	var captured []audit.AuditEvent
	appendAuditEvent = func(path string, event audit.AuditEvent) error {
		captured = append(captured, event)
		return nil
	}

	var out bytes.Buffer
	w := Workflow{
		opts: Options{BackupDir: "/backups", Yes: true, AuditLog: "/tmp/audit.log"},
		out:  &out,
		lc:   &mockLifecycle{},
	}
	if err := w.Cleanup("id-1"); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if !strings.Contains(out.String(), "Backup id-1 removed") {
		t.Fatalf("cleanup output = %q", out.String())
	}
	if len(captured) != 1 || captured[0].Action != "rollback.cleanup" || !captured[0].Success || captured[0].BackupID != "id-1" {
		t.Fatalf("unexpected audit event: %+v", captured)
	}
}

func TestWorkflowCleanupErrorWritesAudit(t *testing.T) {
	oldAppend := appendAuditEvent
	t.Cleanup(func() { appendAuditEvent = oldAppend })

	var captured []audit.AuditEvent
	appendAuditEvent = func(path string, event audit.AuditEvent) error {
		captured = append(captured, event)
		return nil
	}

	w := Workflow{
		opts: Options{BackupDir: "/backups", Yes: true, AuditLog: "/tmp/audit.log"},
		out:  &bytes.Buffer{},
		lc:   &mockLifecycle{cleanupErr: errors.New("cleanup failed")},
	}
	if err := w.Cleanup("id"); err == nil || !strings.Contains(err.Error(), "cleanup failed") {
		t.Fatalf("Cleanup err = %v", err)
	}
	if len(captured) != 1 || captured[0].Action != "rollback.cleanup" || captured[0].Success || captured[0].Error != "cleanup failed" {
		t.Fatalf("unexpected audit event: %+v", captured)
	}
}

func TestWorkflowCleanupAuditWriteFailure(t *testing.T) {
	oldAppend := appendAuditEvent
	t.Cleanup(func() { appendAuditEvent = oldAppend })

	appendAuditEvent = func(path string, event audit.AuditEvent) error {
		if event.Success {
			return errors.New("audit write failed")
		}
		return nil
	}

	w := Workflow{
		opts: Options{BackupDir: "/backups", Yes: true, AuditLog: "/tmp/audit.log"},
		out:  &bytes.Buffer{},
		lc:   &mockLifecycle{},
	}
	err := w.Cleanup("id")
	if err == nil || !strings.Contains(err.Error(), "audit log write failed after successful cleanup") {
		t.Fatalf("Cleanup err = %v", err)
	}
}
