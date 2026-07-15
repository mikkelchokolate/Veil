package protocols

import (
	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/protocols/schema"
	"github.com/mikkelchokolate/Veil/internal/runtimeinstall"
	"github.com/mikkelchokolate/Veil/internal/service"
)

// ProtocolPlugin is the central extension point for a protocol.
// The interface uses only primitive or leaf-package return types so that
// protocol implementations live in sub-packages without importing the parent
// protocols package (which would create an import cycle with the registry).
type ProtocolPlugin interface {
	Protocol() string
	DisplayName() string
	Transports() []string
	RequiresCaddy() bool
	FirewallService() string
	MaxEnabled() int
}

// Metadata collects the primitive metadata methods into a struct.
type Metadata struct {
	Protocol        string   `json:"protocol"`
	DisplayName     string   `json:"displayName"`
	Transports      []string `json:"transports"`
	RequiresCaddy   bool     `json:"requiresCaddy"`
	FirewallService string   `json:"firewallService"`
	MaxEnabled      int      `json:"maxEnabled"`
}

// MetadataOf returns a Metadata struct for a plugin.
func MetadataOf(p ProtocolPlugin) Metadata {
	return Metadata{
		Protocol:        p.Protocol(),
		DisplayName:     p.DisplayName(),
		Transports:      append([]string(nil), p.Transports()...),
		RequiresCaddy:   p.RequiresCaddy(),
		FirewallService: p.FirewallService(),
		MaxEnabled:      p.MaxEnabled(),
	}
}

// ConfigRenderer turns enabled inbounds into generated config artifacts.
type ConfigRenderer interface {
	RenderConfig(input generatedconfig.ProtocolRenderInput) ([]generatedconfig.GeneratedConfigArtifact, bool, error)
	ArtifactSpec() generatedconfig.ArtifactSpec
}

// RuntimeProvider describes systemd units and the external binary required by the protocol.
type RuntimeProvider interface {
	RuntimeDescriptors(enabledInbounds []model.Inbound) []service.ManagedRuntime
	RuntimeInstall(arch string) runtimeinstall.Runtime
}

// Validator contributes protocol-specific validation issues.
type Validator interface {
	ValidateSettings(settings model.Settings, inbound model.Inbound) error
	ValidateInbound(settings model.Settings, inbound model.Inbound) []model.ValidationIssue
	NeedsDomain(settings model.Settings, inbound model.Inbound) bool
	HasCredential(settings model.Settings, inbound model.Inbound) bool
	NeedsEmail(settings model.Settings, inbound model.Inbound) bool
}

// ClientAccessProvider builds client links / artifacts for the protocol.
type ClientAccessProvider interface {
	BuildLinks(settings model.Settings, inbound model.Inbound) ([]model.ClientLink, error)
}

// UIProvider contributes dynamic form fields and one-click provisioning.
type UIProvider interface {
	InboundFieldSchema() []schema.FieldSchema
	SettingsFieldSchema() []schema.FieldSchema
	Autofill(inbound model.Inbound) (model.Inbound, error)
}

// RoomGenerator contributes server-side generation of one-click values such as
// meeting room IDs. The API registers a per-protocol /api/{protocol}/room route
// for every plugin that implements this interface.
type RoomGenerator interface {
	GenerateRoom(provider string) (string, error)
}
