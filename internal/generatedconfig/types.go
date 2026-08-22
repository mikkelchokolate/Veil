package generatedconfig

import (
	"github.com/mikkelchokolate/Veil/internal/clientaccess"
	"github.com/mikkelchokolate/Veil/internal/model"
	veilruntime "github.com/mikkelchokolate/Veil/internal/runtime"
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

// BuildClientCredentials resolves the effective client credentials for an
// inbound, including normalized Client+Binding credentials attached as
// runtime-only data. The merge lives in clientaccess.BuildClientCredentials
// so links, subscriptions, live validation and renderers all agree.
func BuildClientCredentials(inbound Inbound) ([]clientaccess.ClientCredential, error) {
	return clientaccess.BuildClientCredentials(inbound)
}
