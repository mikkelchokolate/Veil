package generatedconfig

import (
	"fmt"

	"github.com/mikkelchokolate/Veil/internal/model"
)

type ConfigInput struct {
	ApplyRoot string
	LiveRoot  string
	Settings  Settings
	Inbounds  []Inbound
	Rules     []RoutingRule
	Warp      WarpConfig
}

type ProtocolRegistry struct {
	protocols         []Protocol
	renderSettingKeys []string
}

type Protocol struct {
	Protocol               string
	MaxEnabled             int
	RequiresRenderSettings bool
	Render                 func(ProtocolRenderInput) ([]GeneratedConfigArtifact, bool, error)
	ArtifactSpec           ArtifactSpec
}

type ProtocolRenderInput struct {
	Settings Settings
	Paths    Paths
	Inbounds []Inbound
	Rules    []RoutingRule
	Warp     WarpConfig
}

func NewProtocolRegistry(protocols []Protocol) ProtocolRegistry {
	return NewProtocolRegistryWithRenderSettingKeys(protocols, nil)
}

// NewProtocolRegistryWithRenderSettingKeys creates a registry and carries the
// set of protocol-field keys that the render-settings policy should treat as
// render-relevant. This keeps the policy in sync with the installed protocol
// plugins instead of a hardcoded key list.
func NewProtocolRegistryWithRenderSettingKeys(protocols []Protocol, renderSettingKeys []string) ProtocolRegistry {
	out := make([]Protocol, len(protocols))
	copy(out, protocols)
	keys := make([]string, len(renderSettingKeys))
	copy(keys, renderSettingKeys)
	return ProtocolRegistry{protocols: out, renderSettingKeys: keys}
}

// RenderSettingFieldKeys returns the render-relevant protocol-field keys
// configured for this registry.
func (r ProtocolRegistry) RenderSettingFieldKeys() []string {
	keys := make([]string, len(r.renderSettingKeys))
	copy(keys, r.renderSettingKeys)
	return keys
}

func (r ProtocolRegistry) Validate(settings Settings, inbounds []Inbound) error {
	for _, protocol := range r.protocols {
		if protocol.MaxEnabled <= 0 {
			continue
		}
		count := len(r.enabledInbounds(settings, inbounds, protocol.Protocol))
		if count > protocol.MaxEnabled {
			return fmt.Errorf("multiple enabled %s inbounds are not renderable as a single generated config yet", protocol.Protocol)
		}
	}
	return validateAggregatedMieruUsers(settings, r.enabledInbounds(settings, inbounds, "mieru"))
}

func validateAggregatedMieruUsers(settings Settings, inbounds []Inbound) error {
	if len(inbounds) == 0 {
		return nil
	}
	config, ok, err := NewMieruGeneratedConfigModel(settings).Build(inbounds)
	if err != nil {
		return err
	}
	if !ok || len(config.Users) == 0 {
		return fmt.Errorf("this inbound has no usable client credential")
	}
	return nil
}

func (r ProtocolRegistry) Render(input ConfigInput) (map[string]string, error) {
	if err := r.Validate(input.Settings, input.Inbounds); err != nil {
		return nil, err
	}
	paths := NewPathsWithLiveRoot(input.ApplyRoot, input.LiveRoot)
	configs := map[string]string{}
	for _, protocol := range r.protocols {
		selected := r.enabledInbounds(input.Settings, input.Inbounds, protocol.Protocol)
		renderInbounds := selected
		if protocol.Protocol == "naiveproxy" {
			// The naive renderer owns the consolidated caddy/config.json, which
			// also issues ACME certs for Hysteria2-only domains. It must run
			// whenever a cert consumer exists — not only when a Naive inbound
			// is enabled — or the live Caddy config never includes those names
			// and the cert-sync step fail-closes apply (audit #156).
			renderInbounds = input.Inbounds
			if len(selected) == 0 && !hasCaddyManagedCertConsumer(input.Inbounds) {
				continue
			}
		} else if len(selected) == 0 {
			continue
		}
		if protocol.RequiresRenderSettings && !NewGeneratedRenderSettingsPolicyWithFieldKeys(r.renderSettingKeys).HasRenderSettings(input.Settings, input.Inbounds) {
			continue
		}
		artifacts, ok, err := protocol.Render(ProtocolRenderInput{Settings: input.Settings, Paths: paths, Inbounds: renderInbounds, Rules: input.Rules, Warp: input.Warp})
		if err != nil {
			return nil, err
		}
		if ok {
			for _, artifact := range artifacts {
				configs[artifact.Path] = artifact.Body
			}
		}
	}
	return configs, nil
}

func (r ProtocolRegistry) RenderInbound(settings Settings, paths Paths, inbound Inbound, warp WarpConfig) (GeneratedConfigArtifact, bool, error) {
	protocol, ok := r.protocol(inbound.Protocol)
	if !ok {
		return GeneratedConfigArtifact{}, false, nil
	}
	arts, ok, err := protocol.Render(ProtocolRenderInput{Settings: settings, Paths: paths, Inbounds: []Inbound{inbound}, Warp: warp})
	if err != nil || !ok || len(arts) == 0 {
		return GeneratedConfigArtifact{}, ok, err
	}
	return arts[0], true, nil
}

func (r ProtocolRegistry) protocol(protocol string) (Protocol, bool) {
	for _, candidate := range r.protocols {
		if candidate.Protocol == protocol {
			return candidate, true
		}
	}
	return Protocol{}, false
}

// hasCaddyManagedCertConsumer reports whether any enabled hysteria2 inbound
// declares a per-inbound domain. Such inbounds rely on Caddy-managed ACME
// certificates (NeedsCaddyCertSync), so the consolidated caddy/config.json
// must be rendered even when no Naive inbound is enabled (audit #156).
func hasCaddyManagedCertConsumer(inbounds []Inbound) bool {
	for _, inbound := range inbounds {
		if inbound.Enabled && inbound.Protocol == "hysteria2" && model.InboundDomain(inbound) != "" {
			return true
		}
	}
	return false
}

func (ProtocolRegistry) enabledInbounds(settings Settings, inbounds []Inbound, protocol string) []Inbound {
	selected := []Inbound{}
	for _, inbound := range inbounds {
		if inbound.Enabled && inbound.Protocol == protocol {
			selected = append(selected, inbound)
		}
	}
	return selected
}

// ArtifactSpecs returns the artifact metadata for every registered protocol that
// contributes generated config artifacts. The result preserves registration order.
func (r ProtocolRegistry) ArtifactSpecs() []ArtifactSpec {
	out := make([]ArtifactSpec, 0, len(r.protocols))
	for _, protocol := range r.protocols {
		if protocol.ArtifactSpec.Subpath == "" {
			continue
		}
		out = append(out, protocol.ArtifactSpec)
	}
	return out
}
