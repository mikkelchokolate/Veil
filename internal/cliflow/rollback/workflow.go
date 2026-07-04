package rollback

import (
	"fmt"
	"io"

	"github.com/mikkelchokolate/Veil/internal/audit"
	"github.com/mikkelchokolate/Veil/internal/backup"
)

type Options struct {
	BackupDir string
	Yes       bool
	AuditLog  string
}

// lifecycle abstracts backup.NewLifecycle so tests can inject mocks.
type lifecycle interface {
	List() ([]string, error)
	Restore(backupID string) ([]string, error)
	Cleanup(backupID string) error
}

// newLifecycle is overridable in tests to avoid touching the real filesystem.
var newLifecycle = func(dir string) lifecycle {
	return backup.NewLifecycle(dir)
}

// appendAuditEvent is overridable in tests to simulate audit log failures.
var appendAuditEvent = audit.AppendAuditEvent

type Workflow struct {
	opts Options
	out  io.Writer
	lc   lifecycle
}

func NewWorkflow(opts Options, out io.Writer) Workflow {
	return Workflow{opts: opts, out: out, lc: newLifecycle(opts.BackupDir)}
}

func (w Workflow) List() error {
	if w.opts.BackupDir == "" {
		return fmt.Errorf("--backup-dir is required")
	}
	ids, err := w.lc.List()
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		fmt.Fprintln(w.out, "No backups found")
		return nil
	}
	for _, id := range ids {
		fmt.Fprintln(w.out, id)
	}
	return nil
}

func (w Workflow) Restore(backupID string) error {
	if w.opts.BackupDir == "" {
		return fmt.Errorf("--backup-dir is required")
	}
	if !w.opts.Yes {
		return fmt.Errorf("restore requires --yes to confirm")
	}
	restored, err := w.lc.Restore(backupID)
	if err != nil {
		writeAuditRestore(w.opts.AuditLog, backupID, false, err.Error(), nil)
		return err
	}
	fmt.Fprintln(w.out, "Restored files:")
	for _, path := range restored {
		fmt.Fprintf(w.out, "- %s\n", path)
	}
	if err := writeAuditRestore(w.opts.AuditLog, backupID, true, "", restored); err != nil {
		return fmt.Errorf("audit log write failed after successful restore: %w", err)
	}
	return nil
}

func (w Workflow) Cleanup(backupID string) error {
	if w.opts.BackupDir == "" {
		return fmt.Errorf("--backup-dir is required")
	}
	if !w.opts.Yes {
		return fmt.Errorf("cleanup requires --yes to confirm")
	}
	if err := w.lc.Cleanup(backupID); err != nil {
		_ = writeAuditCleanup(w.opts.AuditLog, backupID, false, err.Error())
		return err
	}
	fmt.Fprintf(w.out, "Backup %s removed\n", backupID)
	if err := writeAuditCleanup(w.opts.AuditLog, backupID, true, ""); err != nil {
		return fmt.Errorf("audit log write failed after successful cleanup: %w", err)
	}
	return nil
}

func writeAuditRestore(auditLog, backupID string, success bool, errMsg string, restoredFiles []string) error {
	return appendAuditEvent(auditLog, audit.AuditEvent{
		Action:        "rollback.restore",
		BackupID:      backupID,
		Success:       success,
		Error:         errMsg,
		RestoredFiles: restoredFiles,
	})
}

func writeAuditCleanup(auditLog, backupID string, success bool, errMsg string) error {
	return appendAuditEvent(auditLog, audit.AuditEvent{
		Action:   "rollback.cleanup",
		BackupID: backupID,
		Success:  success,
		Error:    errMsg,
	})
}
