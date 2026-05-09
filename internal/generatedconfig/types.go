package generatedconfig

import (
	"github.com/veil-panel/veil/internal/clientaccess"
	"github.com/veil-panel/veil/internal/model"
	veilruntime "github.com/veil-panel/veil/internal/runtime"
)

type Settings = model.Settings
type Inbound = model.Inbound
type ClientProfile = model.ClientProfile
type RoutingRule = model.RoutingRule
type WarpConfig = model.WarpConfig
type RoutingSource = model.RoutingSource
type RoutingSourceFile = model.RoutingSourceFile
type ConfigValidationResult = model.ConfigValidationResult
type RuntimeCommandInput = veilruntime.RuntimeCommandInput
type RuntimeCommandOutput = veilruntime.RuntimeCommandOutput
type RuntimeCommandExecutor = veilruntime.RuntimeCommandExecutor

type GeneratedConfigArtifact struct {
	Path string
	Body string
}

func NewRuntimeCommandExecutor() RuntimeCommandExecutor {
	return veilruntime.NewRuntimeCommandExecutor()
}

func BuildClientCredentials(inbound Inbound) ([]clientaccess.ClientCredential, error) {
	return clientaccess.BuildClientCredentials(inbound)
}
