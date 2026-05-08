package api

import "strings"

type PanelEventBinding struct {
	ElementID string
	Handler   string
	Event     string
}

type PanelEventBindingCatalog struct{}

func NewPanelEventBindingCatalog() PanelEventBindingCatalog { return PanelEventBindingCatalog{} }

func (PanelEventBindingCatalog) Bindings() []PanelEventBinding {
	bindings := []PanelEventBinding{
		{ElementID: "settings-form", Handler: "saveSettings", Event: "submit"},
		{ElementID: "load-settings", Handler: "loadSettingsIntoForm", Event: "click"},
		{ElementID: "load-service-status", Handler: "loadServiceStatus", Event: "click"},
		{ElementID: "load-client-links", Handler: "loadClientLinks", Event: "click"},
		{ElementID: "load-client-subscription", Handler: "loadClientSubscription", Event: "click"},
		{ElementID: "load-client-subscription-raw", Handler: "loadRawClientSubscription", Event: "click"},
		{ElementID: "download-mieru-configs", Handler: "downloadMieruConfigs", Event: "click"},
		{ElementID: "copy-client-links", Handler: "copyClientLinksOutput", Event: "click"},
		{ElementID: "inbound-protocol", Handler: "syncInboundTransportOptions", Event: "change"},
		{ElementID: "inbound-form", Handler: "saveInbound", Event: "submit"},
		{ElementID: "delete-inbound", Handler: "deleteInbound", Event: "click"},
		{ElementID: "load-inbounds", Handler: "loadInboundsIntoOutput", Event: "click"},
		{ElementID: "routing-rule-form", Handler: "saveRoutingRule", Event: "submit"},
		{ElementID: "delete-routing-rule", Handler: "deleteRoutingRule", Event: "click"},
		{ElementID: "apply-routing-preset", Handler: "applyRoutingPreset", Event: "click"},
		{ElementID: "warp-form", Handler: "saveWarpConfig", Event: "submit"},
		{ElementID: "load-warp-config", Handler: "loadWarpIntoForm", Event: "click"},
	}
	return append([]PanelEventBinding(nil), bindings...)
}

func panelEventBindingCatalogJS() string {
	var b strings.Builder
	for _, binding := range NewPanelEventBindingCatalog().Bindings() {
		b.WriteString("    document.getElementById('")
		b.WriteString(binding.ElementID)
		b.WriteString("').addEventListener('")
		b.WriteString(binding.Event)
		b.WriteString("', ")
		b.WriteString(binding.Handler)
		b.WriteString(");\n")
	}
	return b.String()
}
