package generatedconfig

import "testing"

func TestGeneratedRenderSettingsPolicyDetectsRenderableSettings(t *testing.T) {
	policy := NewGeneratedRenderSettingsPolicy()
	if policy.HasRenderSettings(Settings{}) {
		t.Fatal("empty settings should not be renderable")
	}
	if !policy.HasRenderSettings(Settings{Domain: "example.com"}) {
		t.Fatal("domain should make settings renderable")
	}
	if !policy.HasRenderSettings(Settings{Hysteria2Password: "secret"}) {
		t.Fatal("protocol secret should make settings renderable")
	}
}
