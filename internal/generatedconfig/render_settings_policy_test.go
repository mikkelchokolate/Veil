package generatedconfig

import "testing"

func TestGeneratedRenderSettingsPolicyDetectsRenderableSettings(t *testing.T) {
	policy := NewGeneratedRenderSettingsPolicy()
	if policy.HasRenderSettings(Settings{}, nil) {
		t.Fatal("empty settings should not be renderable")
	}
	if !policy.HasRenderSettings(Settings{Domain: "example.com"}, nil) {
		t.Fatal("domain should make settings renderable")
	}
	if !policy.HasRenderSettings(Settings{Hysteria2Password: "secret"}, nil) {
		t.Fatal("protocol secret should make settings renderable")
	}
}

func TestGeneratedRenderSettingsPolicyDetectsProtocolFields(t *testing.T) {
	policy := NewGeneratedRenderSettingsPolicy()
	settings := Settings{ProtocolFields: map[string]any{"hysteria2Password": "secret"}}
	if !policy.HasRenderSettings(settings, nil) {
		t.Fatal("protocolFields secret should make settings renderable")
	}
}

func TestGeneratedRenderSettingsPolicyDetectsInboundCredentials(t *testing.T) {
	policy := NewGeneratedRenderSettingsPolicy()
	inbounds := []Inbound{{Name: "hy2", Protocol: "hysteria2", Enabled: true, Password: "secret"}}
	if !policy.HasRenderSettings(Settings{}, inbounds) {
		t.Fatal("inbound password should make settings renderable")
	}
}

func TestGeneratedRenderSettingsPolicyDetectsInboundProtocolFields(t *testing.T) {
	policy := NewGeneratedRenderSettingsPolicy()
	inbounds := []Inbound{{Name: "hy2", Protocol: "hysteria2", Enabled: true, ProtocolFields: map[string]any{"hysteria2Password": "secret"}}}
	if !policy.HasRenderSettings(Settings{}, inbounds) {
		t.Fatal("inbound protocolFields secret should make settings renderable")
	}
}
