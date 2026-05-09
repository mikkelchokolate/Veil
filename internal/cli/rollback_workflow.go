package cli

import (
	"io"

	rollbackflow "github.com/veil-panel/veil/internal/cliflow/rollback"
)

type rollbackWorkflowOptions struct {
	BackupDir string
	Yes       bool
	AuditLog  string
}

type RollbackWorkflow struct {
	inner rollbackflow.Workflow
}

func NewRollbackWorkflow(opts rollbackWorkflowOptions, out io.Writer) RollbackWorkflow {
	return RollbackWorkflow{inner: rollbackflow.NewWorkflow(rollbackflow.Options{BackupDir: opts.BackupDir, Yes: opts.Yes, AuditLog: opts.AuditLog}, out)}
}

func (w RollbackWorkflow) List() error {
	return w.inner.List()
}

func (w RollbackWorkflow) Restore(backupID string) error {
	return w.inner.Restore(backupID)
}

func (w RollbackWorkflow) Cleanup(backupID string) error {
	return w.inner.Cleanup(backupID)
}
