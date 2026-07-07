package api

import (
	"path/filepath"

	"github.com/mikkelchokolate/Veil/internal/applyplan"
	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/panelaccess"
	"github.com/mikkelchokolate/Veil/internal/protocols"
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
	panelAccessIntent := panelaccess.New(input.Settings, protocols.NewCatalog().RequiresCaddy).ApplyIntent(input.Inbounds)
	capabilities := []applyplan.ProtocolCapability{}
	catalog := NewApplyProtocolCapabilityCatalog()
	for _, protocolCapability := range catalog.All() {
		capability := protocolCapability
		capabilities = append(capabilities, applyplan.ProtocolCapability{
			Protocol:               capability.Protocol,
			Config:                 capability.Config,
			Action:                 capability.Action,
			ActionForInbound:       actionForInboundRuntime,
			ValidateSettings:       capability.ValidateSettings,
			ValidateInboundRender:  capability.ValidateInboundRender,
			RequiresRenderSettings: capability.RequiresRenderSettings,
		})
	}
	runtimeCatalog := NewManagedRuntimeCatalogFor(input.Inbounds, input.Warp)
	runtimeUnits := service.NewProtocolRuntimeProvisioning(runtimeCatalog).Plan(input.Inbounds, input.Warp).SystemdUnits()
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
		PanelAccess: applyplan.Material{
			Configs:  panelAccessIntent.Configs,
			Actions:  panelAccessIntent.Actions,
			Runtimes: panelAccessIntent.Runtimes,
			Errors:   panelAccessIntent.Errors,
		},
		Capabilities: capabilities,
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

func actionForInboundRuntime(inbound Inbound) string {
	p, ok := protocols.NewRegistry().Get(inbound.Protocol)
	if !ok {
		return ""
	}
	rp, ok := protocols.AsRuntimeProvider(p)
	if !ok {
		return ""
	}
	for _, descriptor := range rp.RuntimeDescriptors([]Inbound{inbound}) {
		if descriptor.Unit == "" || descriptor.PromotedVerb == "" {
			continue
		}
		return descriptor.PromotedVerb + " " + descriptor.Unit
	}
	return ""
}
