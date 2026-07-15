package api

import (
	"fmt"
	"net"
	"path/filepath"
	"strconv"
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
	runtimeCatalog := NewManagedRuntimeCatalogFor(input.Settings, input.Inbounds, input.Warp)
	caddyMaterial := buildCaddyMaterial(input.Settings, input.Inbounds, input.Warp, runtimeCatalog)
	capabilities := []applyplan.ProtocolCapability{}
	catalog := NewApplyProtocolCapabilityCatalog()
	for _, protocolCapability := range catalog.All() {
		capability := protocolCapability
		capabilities = append(capabilities, applyplan.ProtocolCapability{
			Protocol:               capability.Protocol,
			Config:                 capability.Config,
			ConfigForInbound:       configForInboundRuntime,
			Action:                 capability.Action,
			ActionForInbound:       actionForInboundRuntime,
			ValidateSettings:       capability.ValidateSettings,
			ValidateInboundRender:  capability.ValidateInboundRender,
			RequiresRenderSettings: capability.RequiresRenderSettings,
		})
	}
	runtimeUnits := filterCaddyRuntimeUnits(service.NewProtocolRuntimeProvisioning(runtimeCatalog).Plan(input.Inbounds, input.Warp).SystemdUnits())
	warpAction := ""
	if action, ok := runtimeCatalog.ApplyAction("sing-box"); ok {
		warpAction = action
	}
	plan := applyplan.Build(applyplan.Input{
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
		ValidateInboundRender: input.ValidateInboundRender,
		ValidateWarpRender:    input.ValidateWarpRender,
		GeneratedRoot:         filepath.Join(applyRoot, "generated"),
		LiveRoot:              filepath.Join(applyRoot, "live"),
	})
	appendProtocolInboundValidation(&plan, catalog, input.Settings, input.Inbounds)
	return plan
}

func appendProtocolInboundValidation(plan *ApplyPlanResponse, catalog ApplyProtocolCapabilityCatalog, settings Settings, inbounds []Inbound) {
	if plan == nil {
		return
	}
	for _, inbound := range inbounds {
		if !inbound.Enabled || !protocolInboundValidationReady(settings, inbound) {
			continue
		}
		capability, ok := catalog.ForProtocol(inbound.Protocol)
		if !ok {
			continue
		}
		for _, issue := range capability.ValidateInbound(settings, inbound) {
			plan.Issues = append(plan.Issues, issue)
			if issue.Severity != "error" {
				continue
			}
			plan.Valid = false
			message := issue.Message
			if message == "" {
				message = issue.Code
			}
			plan.Errors = appendUniqueApplyPlanError(plan.Errors, message)
		}
	}
}

func protocolInboundValidationReady(settings Settings, inbound Inbound) bool {
	p, ok := protocols.NewRegistry().Get(inbound.Protocol)
	if !ok {
		return false
	}
	validator, ok := protocols.AsValidator(p)
	if !ok {
		return false
	}
	if validator.NeedsDomain(settings, inbound) {
		domain := strings.TrimSpace(settings.Domain)
		if inbound.Protocol == "naiveproxy" {
			domain = resolveNaiveDomain(inbound, settings)
		} else if value := strings.TrimSpace(inboundDomain(inbound)); value != "" {
			domain = value
		}
		if domain == "" {
			return false
		}
	}
	if validator.NeedsEmail(settings, inbound) {
		email := strings.TrimSpace(settings.Email)
		if inbound.Protocol == "naiveproxy" {
			email = resolveNaiveEmail(inbound, settings)
		}
		if email == "" {
			return false
		}
	}
	return true
}

func appendUniqueApplyPlanError(errors []string, message string) []string {
	if message == "" {
		return errors
	}
	for _, existing := range errors {
		if existing == message {
			return errors
		}
	}
	return append(errors, message)
}

func configForInboundRuntime(inbound Inbound) string {
	if protocols.NewCatalog().RequiresCaddy(inbound.Protocol) {
		return ""
	}
	for _, descriptor := range runtimeDescriptorsForInbound(inbound) {
		if descriptor.PromotedSubpath == "" {
			continue
		}
		return filepath.ToSlash(filepath.Join("/etc/veil", "generated", filepath.FromSlash(descriptor.PromotedSubpath)))
	}
	return ""
}

func actionForInboundRuntime(inbound Inbound) string {
	if protocols.NewCatalog().RequiresCaddy(inbound.Protocol) {
		return ""
	}
	for _, descriptor := range runtimeDescriptorsForInbound(inbound) {
		if descriptor.Unit == "" || descriptor.PromotedVerb == "" {
			continue
		}
		return descriptor.PromotedVerb + " " + descriptor.Unit
	}
	return ""
}

func runtimeDescriptorsForInbound(inbound Inbound) []ManagedRuntime {
	p, ok := protocols.NewRegistry().Get(inbound.Protocol)
	if !ok {
		return nil
	}
	rp, ok := protocols.AsRuntimeProvider(p)
	if !ok {
		return nil
	}
	return rp.RuntimeDescriptors([]Inbound{inbound})
}

func buildCaddyMaterial(settings Settings, inbounds []Inbound, warp WarpConfig, runtimeCatalog ManagedRuntimeCatalog) applyplan.Material {
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

	for _, inb := range inbounds {
		if inb.Protocol != "naiveproxy" || !inb.Enabled {
			continue
		}
		if domain := resolveNaiveDomain(inb, settings); domain == "" {
			material.Errors = append(material.Errors, fmt.Sprintf("naive inbound %q is missing a public domain", inb.Name))
			return material
		}
		if email := resolveNaiveEmail(inb, settings); email == "" {
			material.Errors = append(material.Errors, fmt.Sprintf("naive inbound %q is missing an ACME email", inb.Name))
			return material
		}
		if !naiveHasCredential(inb, settings) {
			material.Errors = append(material.Errors, fmt.Sprintf("naive inbound %q is missing valid credentials (enable a profile with username/password or set naive username/password)", inb.Name))
			return material
		}
	}

	renderSettings := settings
	if renderSettings.DefaultAcmeEmail == "" && renderSettings.PanelEmail == "" {
		renderSettings.DefaultAcmeEmail = renderSettings.Email
	}
	plan, owners, challengeIssues, err := caddyassembly.BuildFinalRenderPlan(renderSettings, inbounds)
	if err != nil {
		material.Errors = append(material.Errors, err.Error())
		return material
	}
	for _, issue := range challengeIssues {
		if issue.Severity == "error" {
			material.Errors = append(material.Errors, issue.Message)
		}
	}

	for _, conflict := range addPanelDirectBindOwner(settings, owners) {
		material.Errors = append(material.Errors, conflict.Message)
	}
	for _, conflict := range addInboundBindOwners(inbounds, owners, runtimeCatalog) {
		material.Errors = append(material.Errors, conflict.Message)
	}
	for _, conflict := range bindregistry.ValidateNoConflicts(owners) {
		material.Errors = append(material.Errors, conflict.Message)
	}
	if warp.Enabled {
		socksPort := warp.SocksPort
		if socksPort == 0 {
			socksPort = 40000
		}
		for key, owner := range plan.Servers {
			if owner.Kind == caddyassembly.CaddyOwnerNaive {
				owner.Upstream = fmt.Sprintf("socks5://127.0.0.1:%d", socksPort)
				plan.Servers[key] = owner
			}
		}
	}

	caps, err := caddycapabilities.Probe("")
	if err != nil {
		material.Errors = append(material.Errors, fmt.Sprintf("failed to probe Caddy capabilities: %v", err))
		return material
	}
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

func addPanelDirectBindOwner(settings Settings, owners map[bindregistry.BindKey]bindregistry.BindOwner) []bindregistry.Conflict {
	if settings.PanelAccess != "direct" {
		return nil
	}
	host, portText, err := net.SplitHostPort(settings.PanelListen)
	if err != nil {
		return nil
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 || port > 65535 {
		return nil
	}
	key := bindregistry.BindKey{Address: host, Port: port, Network: bindregistry.ListenTCP}
	owner := bindregistry.BindOwner{Kind: bindregistry.BindOwnerPanelDirect, ServiceName: "veil.service"}
	if existing, ok := owners[key]; ok && existing != owner {
		return []bindregistry.Conflict{{
			Key:     key,
			Owners:  []bindregistry.BindOwner{existing, owner},
			Message: fmt.Sprintf("TCP %s:%d is claimed by Panel direct listener and another service", key.Address, key.Port),
		}}
	}
	owners[key] = owner
	return nil
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
			continue
		case "hysteria2":
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
	return strings.TrimSpace(v)
}

func resolveNaiveDomain(inb Inbound, settings Settings) string {
	if inb.ProtocolFields != nil {
		if d, ok := inb.ProtocolFields["domain"].(string); ok {
			if v := strings.TrimSpace(d); v != "" {
				return v
			}
		}
	}
	return strings.TrimSpace(settings.Domain)
}

func resolveNaiveEmail(inb Inbound, settings Settings) string {
	candidates := []string{}
	if inb.ProtocolFields != nil {
		if e, ok := inb.ProtocolFields["email"].(string); ok {
			candidates = append(candidates, e)
		}
	}
	candidates = append(candidates, settings.DefaultAcmeEmail, settings.PanelEmail, settings.Email)
	for _, candidate := range candidates {
		if value := strings.TrimSpace(candidate); value != "" {
			return value
		}
	}
	return ""
}

func naiveHasCredential(inb Inbound, settings Settings) bool {
	for _, profile := range inb.Profiles {
		if !profile.Enabled {
			continue
		}
		username := strings.TrimSpace(profile.Username)
		if username == "" {
			username = strings.TrimSpace(profile.Name)
		}
		if username != "" && strings.TrimSpace(profile.Password) != "" {
			return true
		}
	}
	username := ""
	password := ""
	if inb.ProtocolFields != nil {
		if value, ok := inb.ProtocolFields["naiveUsername"].(string); ok {
			username = strings.TrimSpace(value)
		}
		if value, ok := inb.ProtocolFields["naivePassword"].(string); ok {
			password = strings.TrimSpace(value)
		}
	}
	if username == "" {
		username = strings.TrimSpace(inb.NaiveUsername)
	}
	if password == "" {
		password = strings.TrimSpace(inb.NaivePassword)
	}
	if username == "" && settings.ProtocolFields != nil {
		if value, ok := settings.ProtocolFields["naiveUsername"].(string); ok {
			username = strings.TrimSpace(value)
		}
	}
	if password == "" && settings.ProtocolFields != nil {
		if value, ok := settings.ProtocolFields["naivePassword"].(string); ok {
			password = strings.TrimSpace(value)
		}
	}
	if username == "" {
		username = strings.TrimSpace(settings.NaiveUsername)
	}
	if password == "" {
		password = strings.TrimSpace(settings.NaivePassword)
	}
	return username != "" && password != ""
}

func filterCaddyRuntimeUnits(units []string) []string {
	out := make([]string, 0, len(units))
	for _, unit := range units {
		if unit == renderer.UnitCaddy || !isTemplateCaddyUnit(unit) {
			out = append(out, unit)
		}
	}
	return out
}

func isTemplateCaddyUnit(unit string) bool {
	return strings.HasPrefix(unit, "veil-caddy@") && strings.HasSuffix(unit, ".service")
}
