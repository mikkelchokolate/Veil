package protocols

import "testing"

type renderPolicyPlugin struct {
	*mockPlugin
	requires bool
}

func (p renderPolicyPlugin) RequiresRenderSettings() bool { return p.requires }

func TestRequiresRenderSettingsDefault(t *testing.T) {
	plugin := &mockPlugin{protocol: "default-render-policy", transports: []string{"tcp"}}
	if !RequiresRenderSettings(plugin) {
		t.Fatal("expected default true")
	}
}

func TestRequiresRenderSettingsOverride(t *testing.T) {
	plugin := renderPolicyPlugin{mockPlugin: &mockPlugin{protocol: "local-render", transports: []string{"tcp"}}, requires: false}
	if RequiresRenderSettings(plugin) {
		t.Fatal("expected override false")
	}
}
