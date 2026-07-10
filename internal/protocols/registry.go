package protocols

import (
	"sort"

	"github.com/mikkelchokolate/Veil/internal/protocols/schema"
)

// Registry holds all registered protocol plugins.
type Registry struct {
	byProtocol map[string]ProtocolPlugin
	order      []string
}

// NewRegistryRaw creates an empty registry.
func NewRegistryRaw() *Registry {
	return &Registry{byProtocol: map[string]ProtocolPlugin{}}
}

// Register adds a plugin to the registry. Panics on duplicate protocol key.
func (r *Registry) Register(p ProtocolPlugin) {
	protocol := p.Protocol()
	if _, exists := r.byProtocol[protocol]; exists {
		panic("duplicate protocol plugin: " + protocol)
	}
	r.byProtocol[protocol] = p
	r.order = append(r.order, protocol)
}

// Get returns a plugin by protocol key.
func (r *Registry) Get(protocol string) (ProtocolPlugin, bool) {
	p, ok := r.byProtocol[protocol]
	return p, ok
}

// All returns plugins in registration order.
func (r *Registry) All() []ProtocolPlugin {
	out := make([]ProtocolPlugin, 0, len(r.order))
	for _, protocol := range r.order {
		out = append(out, r.byProtocol[protocol])
	}
	return out
}

// Protocols returns the sorted list of protocol keys.
func (r *Registry) Protocols() []string {
	out := append([]string(nil), r.order...)
	sort.Strings(out)
	return out
}

// Choices returns lightweight metadata used by the Panel and API.
func (r *Registry) Choices() []Choice {
	out := make([]Choice, 0, len(r.order))
	for _, protocol := range r.order {
		meta := MetadataOf(r.byProtocol[protocol])
		out = append(out, Choice{
			Protocol:        meta.Protocol,
			DisplayName:     meta.DisplayName,
			Transports:      append([]string(nil), meta.Transports...),
			FirewallService: meta.FirewallService,
			RequiresCaddy:   meta.RequiresCaddy,
		})
	}
	return out
}

// Metadata returns the metadata for a protocol, or zero values if unknown.
func (r *Registry) Metadata(protocol string) Metadata {
	if p, ok := r.byProtocol[protocol]; ok {
		return MetadataOf(p)
	}
	return Metadata{}
}

// SupportsTransport reports whether the protocol supports the given transport.
func (r *Registry) SupportsTransport(protocol, transport string) bool {
	meta := r.Metadata(protocol)
	for _, t := range meta.Transports {
		if t == transport {
			return true
		}
	}
	return false
}

// FirewallService returns the firewall service name for a protocol, if any.
func (r *Registry) FirewallService(protocol string) (string, bool) {
	meta := r.Metadata(protocol)
	if meta.FirewallService == "" {
		return "", false
	}
	return meta.FirewallService, true
}

// RequiresCaddy reports whether any registered protocol requires Caddy.
func (r *Registry) RequiresCaddy() bool {
	for _, p := range r.All() {
		if p.RequiresCaddy() {
			return true
		}
	}
	return false
}

// AsConfigRenderer returns the ConfigRenderer capability or nil.
func AsConfigRenderer(p ProtocolPlugin) (ConfigRenderer, bool) {
	c, ok := p.(ConfigRenderer)
	return c, ok
}

// AsRuntimeProvider returns the RuntimeProvider capability or nil.
func AsRuntimeProvider(p ProtocolPlugin) (RuntimeProvider, bool) {
	c, ok := p.(RuntimeProvider)
	return c, ok
}

// AsValidator returns the Validator capability or nil.
func AsValidator(p ProtocolPlugin) (Validator, bool) {
	c, ok := p.(Validator)
	return c, ok
}

// AsClientAccessProvider returns the ClientAccessProvider capability or nil.
func AsClientAccessProvider(p ProtocolPlugin) (ClientAccessProvider, bool) {
	c, ok := p.(ClientAccessProvider)
	return c, ok
}

// AsClientAccessAggregator returns the ClientAccessAggregator capability or nil.
func AsClientAccessAggregator(p ProtocolPlugin) (ClientAccessAggregator, bool) {
	c, ok := p.(ClientAccessAggregator)
	return c, ok
}

// AsUIProvider returns the UIProvider capability or nil.
func AsUIProvider(p ProtocolPlugin) (UIProvider, bool) {
	c, ok := p.(UIProvider)
	return c, ok
}

// AsRoomGenerator returns the RoomGenerator capability or nil.
func AsRoomGenerator(p ProtocolPlugin) (RoomGenerator, bool) {
	c, ok := p.(RoomGenerator)
	return c, ok
}

// SettingsFieldSchemas returns the aggregate settings-scoped field schemas from
// every registered plugin. This lets protocol-agnostic code such as settings
// validation/redaction know which protocol-specific keys exist and which ones
// are secrets.
func (r *Registry) SettingsFieldSchemas() []schema.FieldSchema {
	out := make([]schema.FieldSchema, 0)
	for _, protocol := range r.order {
		plugin := r.byProtocol[protocol]
		ui, ok := AsUIProvider(plugin)
		if !ok {
			continue
		}
		for _, f := range ui.SettingsFieldSchema() {
			if f.Scope == "" || f.Scope == "settings" {
				out = append(out, f)
			}
		}
	}
	return out
}
