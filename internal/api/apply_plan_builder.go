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
	if input.Settings.Stack != "both" && input.Settings.Stack != "naive" && input.Settings.Stack != "hysteria2" {
		plan.Errors = append(plan.Errors, "unsupported stack: "+input.Settings.Stack)
	}
	seen := map[string]bool{}
	for _, inbound := range input.Inbounds {
		if !inbound.Enabled || !stackIncludesProtocol(input.Settings.Stack, inbound.Protocol) {
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
		switch inbound.Protocol {
		case "naiveproxy":
			if err := NewNaiveCaddySettingsRequirement().Validate(input.Settings); err != nil {
				plan.Errors = append(plan.Errors, err.Error())
			}
			plan.Configs = appendUnique(plan.Configs, "/etc/veil/generated/caddy/Caddyfile")
			plan.Actions = appendUnique(plan.Actions, "reload veil-naive.service")
			if input.RenderSettingsAvailable && input.ValidateInboundRender != nil {
				if err := input.ValidateInboundRender(inbound); err != nil {
					plan.Errors = append(plan.Errors, err.Error())
				}
			}
		case "hysteria2":
			plan.Configs = appendUnique(plan.Configs, "/etc/veil/generated/hysteria2/server.yaml")
			plan.Actions = appendUnique(plan.Actions, "reload veil-hysteria2.service")
			if input.RenderSettingsAvailable && input.ValidateInboundRender != nil {
				if err := input.ValidateInboundRender(inbound); err != nil {
					plan.Errors = append(plan.Errors, err.Error())
				}
			}
		case "mieru":
			plan.Configs = appendUnique(plan.Configs, "/etc/veil/generated/mieru/server_config.json")
			plan.Actions = appendUnique(plan.Actions, "reload veil-mieru.service")
		default:
			if inbound.Protocol != "" {
				plan.Errors = append(plan.Errors, "unsupported inbound protocol: "+inbound.Protocol)
			}
		}
	}
	if err := validateGeneratedConfigInboundCardinality(input.Settings, input.Inbounds); err != nil {
		plan.Errors = append(plan.Errors, err.Error())
	}
	if input.Warp.Enabled {
		plan.Configs = appendUnique(plan.Configs, "/etc/veil/generated/sing-box/warp.json")
		plan.Actions = appendUnique(plan.Actions, "reload veil-warp.service")
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
