package api

import (
	"strings"
	"testing"
)

func TestGeneratedInboundConfigRendererRendersEnabledInboundArtifact(t *testing.T) {
	renderer := NewGeneratedInboundConfigRenderer(Settings{
		Domain:        "example.com",
		Email:         "admin@example.com",
		NaiveUsername: "veil",
		NaivePassword: "global-secret",
		FallbackRoot:  "/var/lib/veil/www",
	}, NewGeneratedConfigPaths("/apply"))
	artifact, ok, err := renderer.Render(Inbound{Name: "naive", Protocol: "naiveproxy", Port: 443, Enabled: true})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !ok {
		t.Fatal("expected artifact")
	}
	if artifact.Path != NewGeneratedConfigPaths("/apply").Caddyfile() {
		t.Fatalf("path = %q", artifact.Path)
	}
	if !strings.Contains(artifact.Body, "global-secret") {
		t.Fatalf("body missing fallback password:\n%s", artifact.Body)
	}
}

func TestGeneratedInboundConfigRendererSkipsDisabledAndUnsupportedProtocol(t *testing.T) {
	renderer := NewGeneratedInboundConfigRenderer(Settings{}, NewGeneratedConfigPaths("/apply"))
	for _, inbound := range []Inbound{
		{Name: "disabled", Protocol: "naiveproxy", Enabled: false},
		{Name: "unsupported", Protocol: "unknown", Enabled: true},
	} {
		if _, ok, err := renderer.Render(inbound); err != nil || ok {
			t.Fatalf("Render(%+v) ok=%v err=%v", inbound, ok, err)
		}
	}
}
