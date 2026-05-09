package applyplan

import (
	"fmt"

	"github.com/veil-panel/veil/internal/model"
)

type Material struct {
	Configs  []string
	Actions  []string
	Runtimes []string
	Errors   []string
}

type ProtocolCapability struct {
	Protocol               string
	Config                 string
	Action                 string
	ValidateSettings       func(model.Settings) error
	ValidateInboundRender  bool
	RequiresRenderSettings bool
}

type Input struct {
	Settings                model.Settings
	Inbounds                []model.Inbound
	Rules                   []model.RoutingRule
	RoutingSource           model.RoutingSource
	Warp                    model.WarpConfig
	RenderSettingsAvailable bool
	PanelAccess             Material
	Capabilities            []ProtocolCapability
	ValidateCardinality     func(model.Settings, []model.Inbound) error
	RuntimeUnits            []string
	WarpAction              string
	ValidateInboundRender   func(model.Inbound) error
	ValidateWarpRender      func() error
}

func Build(input Input) model.ApplyPlanResponse {
	plan := model.ApplyPlanResponse{
		Valid:   true,
		Configs: []string{},
		Actions: []string{"validate management state"},
	}
	appendMaterial(&plan, input.PanelAccess)
	seen := map[string]bool{}
	capabilities := protocolCapabilities(input.Capabilities)
	for _, inbound := range input.Inbounds {
		if !inbound.Enabled {
			continue
		}
		if inbound.Name == "" || inbound.Protocol == "" || inbound.Transport == "" {
			plan.Errors = append(plan.Errors, "enabled inbounds require name, protocol, and transport")
		}
		if inbound.Port <= 0 {
			plan.Errors = append(plan.Errors, "enabled inbounds require a positive port")
		}
		key := inbound.Transport + ":" + fmt.Sprint(inbound.Port)
		if seen[key] {
			plan.Errors = append(plan.Errors, "duplicate enabled inbound transport/port")
		}
		seen[key] = true
		capability, ok := capabilities[inbound.Protocol]
		if !ok {
			if inbound.Protocol != "" {
				plan.Errors = append(plan.Errors, "unsupported inbound protocol: "+inbound.Protocol)
			}
			continue
		}
		if capability.ValidateSettings != nil {
			if err := capability.ValidateSettings(input.Settings); err != nil {
				plan.Errors = append(plan.Errors, err.Error())
			}
		}
		plan.Configs = appendUnique(plan.Configs, capability.Config)
		plan.Actions = appendUnique(plan.Actions, capability.Action)
		if capability.ShouldValidateRender(input.RenderSettingsAvailable) && input.ValidateInboundRender != nil {
			if err := input.ValidateInboundRender(inbound); err != nil {
				plan.Errors = append(plan.Errors, err.Error())
			}
		}
	}
	if input.ValidateCardinality != nil {
		if err := input.ValidateCardinality(input.Settings, input.Inbounds); err != nil {
			plan.Errors = append(plan.Errors, err.Error())
		}
	}
	for _, unit := range input.RuntimeUnits {
		plan.Runtimes = appendUnique(plan.Runtimes, unit)
	}
	if input.Warp.Enabled {
		plan.Configs = appendUnique(plan.Configs, "/etc/veil/generated/sing-box/warp.json")
		if input.WarpAction != "" {
			plan.Actions = appendUnique(plan.Actions, input.WarpAction)
		}
		if input.ValidateWarpRender != nil {
			if err := input.ValidateWarpRender(); err != nil {
				plan.Errors = append(plan.Errors, err.Error())
			}
		}
	}
	for _, rule := range input.Rules {
		if !rule.Enabled {
			continue
		}
		if rule.Name == "" || rule.Match == "" || rule.Outbound == "" {
			plan.Errors = append(plan.Errors, "enabled routing rules require name, match, and outbound")
			continue
		}
		switch rule.Outbound {
		case "direct":
		case "warp":
			if !input.Warp.Enabled {
				plan.Errors = append(plan.Errors, "routing rule "+rule.Name+" requires WARP to be enabled")
			}
		default:
			plan.Errors = append(plan.Errors, "unsupported routing outbound: "+rule.Outbound)
		}
	}
	for _, file := range input.RoutingSource.Files {
		if file.Name == "" || file.URL == "" {
			plan.Errors = append(plan.Errors, "routing source files require name and URL")
			continue
		}
		plan.Configs = appendUnique(plan.Configs, "/etc/veil/generated/rules/"+file.Name)
	}
	if len(plan.Configs) > 0 {
		plan.Actions = append([]string{"validate management state", "stage generated configs"}, plan.Actions[1:]...)
	}
	if len(plan.Errors) > 0 {
		plan.Valid = false
	}
	return plan
}

func appendMaterial(plan *model.ApplyPlanResponse, material Material) {
	for _, config := range material.Configs {
		plan.Configs = appendUnique(plan.Configs, config)
	}
	for _, action := range material.Actions {
		plan.Actions = appendUnique(plan.Actions, action)
	}
	for _, runtime := range material.Runtimes {
		plan.Runtimes = appendUnique(plan.Runtimes, runtime)
	}
	plan.Errors = append(plan.Errors, material.Errors...)
}

func protocolCapabilities(capabilities []ProtocolCapability) map[string]ProtocolCapability {
	byProtocol := map[string]ProtocolCapability{}
	for _, capability := range capabilities {
		byProtocol[capability.Protocol] = capability
	}
	return byProtocol
}

func (c ProtocolCapability) ShouldValidateRender(renderSettingsAvailable bool) bool {
	return c.ValidateInboundRender && (!c.RequiresRenderSettings || renderSettingsAvailable)
}

func appendUnique(values []string, next string) []string {
	if next == "" {
		return values
	}
	for _, value := range values {
		if value == next {
			return values
		}
	}
	return append(values, next)
}
