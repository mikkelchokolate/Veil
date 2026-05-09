package api

import "github.com/veil-panel/veil/internal/panel"

type ApplyWorkflowCommand = panel.ApplyWorkflowCommand
type ApplyWorkflowCommandCatalog = panel.ApplyWorkflowCommandCatalog

func NewApplyWorkflowCommandCatalog() ApplyWorkflowCommandCatalog {
	return panel.NewApplyWorkflowCommandCatalog()
}
