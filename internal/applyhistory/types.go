package applyhistory

import "github.com/mikkelchokolate/Veil/internal/model"

type ApplyResponse = model.ApplyResponse
type ApplyPlanResponse = model.ApplyPlanResponse
type ApplyHistoryEntry = model.ApplyHistoryEntry
type ConfigValidationResult = model.ConfigValidationResult
type ServiceActionResult = model.ServiceActionResult
type ServiceHealthResult = model.ServiceHealthResult

type History = ApplyHistory

func NewHistory(path string, max int) History { return NewApplyHistory(path, max) }
