package cli

import (
	"fmt"
	"io"

	"github.com/veil-panel/veil/internal/installer"
)

type rollbackWorkflowOptions struct {
	BackupDir string
	Yes       bool
	AuditLog  string
}

type RollbackWorkflow struct {
	opts rollbackWorkflowOptions
	out  io.Writer
}

func NewRollbackWorkflow(opts rollbackWorkflowOptions, out io.Writer) RollbackWorkflow {
	return RollbackWorkflow{opts: opts, out: out}
}

func (w RollbackWorkflow) List() error {
	if w.opts.BackupDir == "" {
		return fmt.Errorf("--backup-dir is required")
	}
	ids, err := installer.NewBackupLifecycle(w.opts.BackupDir).List()
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

func (w RollbackWorkflow) Restore(backupID string) error {
	if w.opts.BackupDir == "" {
		return fmt.Errorf("--backup-dir is required")
	}
	if !w.opts.Yes {
		return fmt.Errorf("restore requires --yes to confirm")
	}
	restored, err := installer.NewBackupLifecycle(w.opts.BackupDir).Restore(backupID)
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

func (w RollbackWorkflow) Cleanup(backupID string) error {
	if w.opts.BackupDir == "" {
		return fmt.Errorf("--backup-dir is required")
	}
	if !w.opts.Yes {
		return fmt.Errorf("cleanup requires --yes to confirm")
	}
	if err := installer.NewBackupLifecycle(w.opts.BackupDir).Cleanup(backupID); err != nil {
		_ = writeAuditCleanup(w.opts.AuditLog, backupID, false, err.Error())
		return err
	}
	fmt.Fprintf(w.out, "Backup %s removed\n", backupID)
	if err := writeAuditCleanup(w.opts.AuditLog, backupID, true, ""); err != nil {
		return fmt.Errorf("audit log write failed after successful cleanup: %w", err)
	}
	return nil
}
