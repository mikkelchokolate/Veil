package generatedconfig

import "testing"

// TestNaiveRendererRunsForHysteria2OnlyCertDomain covers audit #156: the
// naive renderer owns the consolidated caddy/config.json and must run
// whenever an enabled hysteria2 inbound carries a domain — even with zero
// enabled naive inbounds — or the live Caddy config never includes the
// Hysteria2-only ACME subjects.
func TestNaiveRendererRunsForHysteria2OnlyCertDomain(t *testing.T) {
	var saw []Inbound
	registry := NewProtocolRegistry([]Protocol{
		{Protocol: "naiveproxy", Render: func(input ProtocolRenderInput) ([]GeneratedConfigArtifact, bool, error) {
			saw = input.Inbounds
			return []GeneratedConfigArtifact{{Path: input.Paths.CaddyJSON(), Body: "{}"}}, true, nil
		}},
	})
	hy2 := Inbound{Name: "hy2", Protocol: "hysteria2", Enabled: true,
		ProtocolFields: map[string]any{"domain": "hy.example.com"}}
	configs, err := registry.Render(ConfigInput{ApplyRoot: "/etc/veil", Inbounds: []Inbound{hy2}})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if len(saw) != 1 || saw[0].Name != "hy2" {
		t.Fatalf("naive renderer was not invoked with all inbounds: %+v", saw)
	}
	if _, ok := configs[NewPaths("/etc/veil").CaddyJSON()]; !ok {
		t.Fatalf("caddy JSON missing: %+v", configs)
	}
}

// TestNaiveRendererSkippedWithoutCertConsumers keeps the previous behavior:
// with no naive inbounds and no hysteria2 cert consumers the renderer stays
// off (audit #156).
func TestNaiveRendererSkippedWithoutCertConsumers(t *testing.T) {
	called := false
	registry := NewProtocolRegistry([]Protocol{
		{Protocol: "naiveproxy", Render: func(ProtocolRenderInput) ([]GeneratedConfigArtifact, bool, error) {
			called = true
			return nil, true, nil
		}},
	})
	_, err := registry.Render(ConfigInput{ApplyRoot: "/etc/veil", Inbounds: []Inbound{
		{Name: "hy-off", Protocol: "hysteria2", Enabled: false, ProtocolFields: map[string]any{"domain": "hy.example.com"}},
		{Name: "hy-nodomain", Protocol: "hysteria2", Enabled: true},
	}})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if called {
		t.Fatal("naive renderer ran without any Caddy cert consumer")
	}
}
