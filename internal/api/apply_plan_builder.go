package api

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/applyplan"
	"github.com/mikkelchokolate/Veil/internal/bindregistry"
	"github.com/mikkelchokolate/Veil/internal/caddyassembly"
	"github.com/mikkelchokolate/Veil/internal/caddycapabilities"
	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/protocols"
	"github.com/mikkelchokolate/Veil/internal/renderer"
	"github.com/mikkelchokolate/Veil/internal/service"
)

type ApplyPlanInput struct {
	ApplyRoot               string
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
	applyRoot := defaultApplyRoot(input.ApplyRoot)
	runtimeCatalog := NewManagedRuntimeCatalogFor(input.Inbounds, input.Warp)
	caddyMaterial := buildCaddyMaterial(input.Settings, input.Inbounds, runtimeCatalog)
	capabilities := []applyplan.ProtocolCapability{}
	catalog := NewApplyProtocolCapabilityCatalog()
	for _, protocolCapability := range catalog.All() {
		capability := protocolCapability
		capabilities = append(capabilities, applyplan.ProtocolCapability{
			Protocol:               capability.Protocol,
			Config:                 capability.Config,
			Action:                 capability.Action,
			ValidateSettings:       capability.ValidateSettings,
			ValidateInboundRender:  capability.ValidateInboundRender,
			RequiresRenderSettings: capability.RequiresRenderSettings,
		})
	}
	runtimeUnits := filterCaddyRuntimeUnits(service.NewProtocolRuntimeProvisioning(runtimeCatalog).Plan(input.Inbounds, input.Warp).SystemdUnits())
	validateInboundRender := input.ValidateInboundRender
	warpAction := ""
	if action, ok := runtimeCatalog.ApplyAction("sing-box"); ok {
		warpAction = action
	}
	return applyplan.Build(applyplan.Input{
		Settings:                input.Settings,
		Inbounds:                input.Inbounds,
		Rules:                   input.Rules,
		RoutingSource:           input.RoutingSource,
		Warp:                    input.Warp,
		RenderSettingsAvailable: input.RenderSettingsAvailable,
		PanelAccess:             caddyMaterial,
		Capabilities:            capabilities,
		ValidateCardinality: func(settings Settings, inbounds []Inbound) error {
			return generatedconfig.NewGeneratedConfigCardinality(settings, protocols.NewGeneratedConfigRegistry()).Validate(inbounds)
		},
		RuntimeUnits:          runtimeUnits,
		WarpAction:            warpAction,
		ValidateInboundRender: validateInboundRender,
		ValidateWarpRender:    input.ValidateWarpRender,
		GeneratedRoot:         filepath.Join(applyRoot, "generated"),
		LiveRoot:              filepath.Join(applyRoot, "live"),
	})
}

func buildCaddyMaterial(settings Settings, inbounds []Inbound, runtimeCatalog ManagedRuntimeCatalog) applyplan.Material {
	material := applyplan.Material{}
	if !caddyRequired(settings, inbounds) {
		return material
	}
	if settings.PanelAccess == "caddy" && (settings.PanelDomain == "" || settings.PanelEmail == "") {
		material.Errors = append(material.Errors, "panelDomain and panelEmail are required for caddy Panel access")
		return material
	}
	if settings.PanelAccess == "caddy" && settings.PanelPublicPort == 0 {
		settings.PanelPublicPort = 443
	}

	initialPlan, owners, err := caddyassembly.BuildRenderPlan(settings, inbounds, nil)
	if err != nil {
		material.Errors = append(material.Errors, err.Error())
		return material
	}

	challengeBinds, issues := caddyassembly.PlanAcmeChallengeBinds(settings.AcmeChallengeMode, initialPlan.Domains, owners)
	for _, issue := range issues {
		if issue.Severity == "error" {
			material.Errors = append(material.Errors, issue.Message)
		}
	}

	allOwners := make(map[bindregistry.BindKey]bindregistry.BindOwner, len(owners)+len(challengeBinds)+len(inbounds))
	for k, v := range owners {
		allOwners[k] = v
	}
	for k := range challengeBinds {
		if existing, ok := allOwners[k]; ok && (existing.Kind == bindregistry.BindOwnerPanelCaddy || existing.Kind == bindregistry.BindOwnerNaive) {
			// Compatible Caddy owner already handles TLS-ALPN-01 on this bind.
			continue
		}
		allOwners[k] = bindregistry.BindOwner{Kind: bindregistry.BindOwnerAcmeChallenge, ServiceName: "veil-caddy.service"}
	}
	for _, conflict := range addInboundBindOwners(inbounds, allOwners, runtimeCatalog) {
		material.Errors = append(material.Errors, conflict.Message)
	}
	for _, conflict := range bindregistry.ValidateNoConflicts(allOwners) {
		material.Errors = append(material.Errors, conflict.Message)
	}

	plan := caddyassembly.CaddyRenderPlan{
		Servers:        initialPlan.Servers,
		ACMEChallenges: challengeBinds,
		Domains:        initialPlan.Domains,
	}

	caps, _ := caddycapabilities.Probe("")
	if _, err := renderer.RenderCaddyJSON(plan, caps); err != nil {
		material.Errors = append(material.Errors, err.Error())
		return material
	}

	path := generatedconfig.ArtifactSpec{Subpath: generatedconfig.CaddyJSONConfigSubpath}.PlanPath()
	material.Configs = append(material.Configs, path)
	material.Actions = append(material.Actions, "reload veil-caddy.service")
	material.Runtimes = append(material.Runtimes, "veil-caddy.service")
	return material
}

func addInboundBindOwners(inbounds []Inbound, owners map[bindregistry.BindKey]bindregistry.BindOwner, runtimeCatalog ManagedRuntimeCatalog) []bindregistry.Conflict {
	var conflicts []bindregistry.Conflict
	for _, inb := range inbounds {
		if !inb.Enabled || inb.Port <= 0 {
			continue
		}
		var key bindregistry.BindKey
		var owner bindregistry.BindOwner
		switch inb.Protocol {
		case "naiveproxy":
			// Already represented by the Caddy render plan owners.
			continue
		case "hysteria2":
			// Hysteria2 public binds are UDP; the TCP port is only used for ACME
			// TLS-ALPN-01 if configured, which is owned by Caddy in this design.
			key = bindregistry.BindKey{Address: "0.0.0.0", Port: inb.Port, Network: bindregistry.ListenUDP}
			owner = bindregistry.BindOwner{Kind: bindregistry.BindOwnerHysteria2, ServiceName: "veil-hysteria2@" + inb.Name + ".service", InboundName: inb.Name}
		default:
			network := bindregistry.ListenTCP
			if inb.Transport == "udp" {
				network = bindregistry.ListenUDP
			}
			key = bindregistry.BindKey{Address: "0.0.0.0", Port: inb.Port, Network: network}
			owner = bindregistry.BindOwner{Kind: bindregistry.BindOwnerInbound, ServiceName: inboundServiceUnit(runtimeCatalog, inb.Protocol, inb.Name), InboundName: inb.Name}
		}
		if existing, ok := owners[key]; ok && existing != owner {
			conflicts = append(conflicts, bindregistry.Conflict{
				Key:     key,
				Owners:  []bindregistry.BindOwner{existing, owner},
				Message: fmt.Sprintf("%s %s:%d is claimed by multiple owners", key.Network, key.Address, key.Port),
			})
			continue
		}
		owners[key] = owner
	}
	return conflicts
}

func inboundServiceUnit(runtimeCatalog ManagedRuntimeCatalog, protocol, inboundName string) string {
	for _, runtime := range runtimeCatalog.Runtimes() {
		if runtime.Protocol != protocol {
			continue
		}
		unit := runtime.Unit
		if idx := strings.Index(unit, "@."); idx != -1 {
			unit = unit[:idx+1] + inboundName + unit[idx+1:]
		}
		return unit
	}
	return "veil-" + protocol + ".service"
}

func caddyRequired(settings Settings, inbounds []Inbound) bool {
	if settings.PanelAccess == "caddy" {
		return true
	}
	for _, inb := range inbounds {
		if !inb.Enabled {
			continue
		}
		if inb.Protocol == "naiveproxy" {
			return true
		}
		if inb.Protocol == "hysteria2" && inboundDomain(inb) != "" {
			return true
		}
	}
	return false
}

func inboundDomain(inb Inbound) string {
	if inb.ProtocolFields == nil {
		return ""
	}
	v, ok := inb.ProtocolFields["domain"].(string)
	if !ok {
		return ""
	}
	return v
}

func filterCaddyRuntimeUnits(units []string) []string {
	out := make([]string, 0, len(units))
	for _, unit := range units {
		if unit == "veil-caddy.service" || !isTemplateCaddyUnit(unit) {
			out = append(out, unit)
		}
	}
	return out
}

func isTemplateCaddyUnit(unit string) bool {
	return len(unit) > len("veil-caddy@") && unit[:len("veil-caddy@")] == "veil-caddy@" && unit[len(unit)-len(".service"):] == ".service"
}
