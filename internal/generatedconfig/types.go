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

func BuildClientCredentials(inbound Inbound) ([]clientaccess.ClientCredential, error) {
	credentials, err := clientaccess.BuildClientCredentials(inbound)
	if err != nil {
		return nil, err
	}
	if len(inbound.RuntimeCredentials) == 0 {
		return credentials, nil
	}
	overrides := make(map[string]struct{}, len(inbound.RuntimeCredentials))
	for _, credential := range inbound.RuntimeCredentials {
		overrides[credential.Username] = struct{}{}
	}
	merged := make([]clientaccess.ClientCredential, 0, len(credentials)+len(inbound.RuntimeCredentials))
	for _, credential := range credentials {
		if _, replaced := overrides[credential.Username]; !replaced {
			merged = append(merged, credential)
		}
	}
	for _, credential := range inbound.RuntimeCredentials {
		merged = append(merged, clientaccess.ClientCredential{Name: credential.Name, Username: credential.Username, Password: credential.Password})
	}
	return merged, nil
}
