package api

import "fmt"

type ApplyPlanInput struct {
	Settings                Settings
	Inbounds                []Inbound
	Rules                   []RoutingRule
	RoutingSource           RoutingSource
	Warp                    WarpConfig
	RenderSettingsAvailable bool
	ValidateInboundRender   func(Inbound) error
	ValidateWarpRender      func() error
}

func BuildApplyPlan(input ApplyPlanInput) ApplyPlanResponse {
	plan := ApplyPlanResponse{
		Valid:   true,
		Configs: []string{},
		Actions: []string{"validate management state"},
	}
	if _, ok := NormalizeSettingsStack(input.Settings.Stack); !ok {
		plan.Errors = append(plan.Errors, "stack must be panel; protocols are configured as Panel inbounds")
	}
	if input.Settings.PanelAccess == "caddy" {
		if input.Settings.Domain == "" || input.Settings.Email == "" {
			plan.Errors = append(plan.Errors, "--domain and --email are required for caddy Panel access")
		} else if _, _, err := NewPanelCaddyAccess().Route(input.Settings); err != nil {
			plan.Errors = append(plan.Errors, err.Error())
		} else {
			plan.Configs = appendUnique(plan.Configs, "/etc/veil/generated/caddy/Caddyfile")
			plan.Actions = appendUnique(plan.Actions, "reload veil-naive.service")
			plan.Runtimes = appendUnique(plan.Runtimes, "veil-naive.service")
		}
	}
	seen := map[string]bool{}
	protocols := NewInboundProtocolCatalog()
	capabilities := NewApplyProtocolCapabilityCatalog()
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
		if input.Settings.PanelAccess == "caddy" && inbound.Transport == "tcp" && inbound.Port == 443 && !protocols.RequiresCaddy(inbound.Protocol) {
			plan.Errors = append(plan.Errors, "panel caddy access uses 443/tcp; choose another TCP port for inbound "+inbound.Name)
		}
		key := inbound.Transport + ":" + fmt.Sprint(inbound.Port)
		if seen[key] {
			plan.Errors = append(plan.Errors, "duplicate enabled inbound transport/port")
		}
		seen[key] = true
		capability, ok := capabilities.ForProtocol(inbound.Protocol)
		if !ok {
			if inbound.Protocol != "" {
				plan.Errors = append(plan.Errors, "unsupported inbound protocol: "+inbound.Protocol)
			}
			continue
		}
		if err := capability.ValidateSettings(input.Settings); err != nil {
			plan.Errors = append(plan.Errors, err.Error())
		}
		plan.Configs = appendUnique(plan.Configs, capability.Config)
		plan.Actions = appendUnique(plan.Actions, capability.Action)
		if capability.ShouldValidateRender(input.RenderSettingsAvailable) && input.ValidateInboundRender != nil {
			if err := input.ValidateInboundRender(inbound); err != nil {
				plan.Errors = append(plan.Errors, err.Error())
			}
		}
	}
	if err := validateGeneratedConfigInboundCardinality(input.Settings, input.Inbounds); err != nil {
		plan.Errors = append(plan.Errors, err.Error())
	}
	for _, unit := range NewProtocolRuntimeProvisioning().Plan(input.Inbounds, input.Warp).SystemdUnits() {
		plan.Runtimes = appendUnique(plan.Runtimes, unit)
	}
	if input.Warp.Enabled {
		plan.Configs = appendUnique(plan.Configs, "/etc/veil/generated/sing-box/warp.json")
		if action, ok := NewManagedRuntimeCatalog().ApplyAction("sing-box"); ok {
			plan.Actions = appendUnique(plan.Actions, action)
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
