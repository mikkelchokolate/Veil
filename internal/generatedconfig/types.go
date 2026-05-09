package generatedconfig

import (
	"github.com/veil-panel/veil/internal/model"
	veilruntime "github.com/veil-panel/veil/internal/runtime"
)

type Settings = model.Settings
type RoutingSource = model.RoutingSource
type RoutingSourceFile = model.RoutingSourceFile
type ConfigValidationResult = model.ConfigValidationResult
type RuntimeCommandInput = veilruntime.RuntimeCommandInput
type RuntimeCommandOutput = veilruntime.RuntimeCommandOutput
type RuntimeCommandExecutor = veilruntime.RuntimeCommandExecutor

func NewRuntimeCommandExecutor() RuntimeCommandExecutor {
	return veilruntime.NewRuntimeCommandExecutor()
}
