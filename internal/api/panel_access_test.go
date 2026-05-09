package api

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPanelAccessBuildsCaddyMaterialAndApplyIntent(t *testing.T) {
	settings := Settings{PanelAccess: "caddy", PanelListen: "127.0.0.1:2096", Domain: "panel.example.com", Email: "admin@example.com", WebBasePath: "/panel-secret/"}
	access := NewPanelAccess(settings)

	artifact, ok, err := access.GeneratedConfig(NewGeneratedConfigPaths("/etc/veil"))
	if err != nil {
		t.Fatalf("GeneratedConfig: %v", err)
	}
	if !ok || artifact.Path != filepath.Join("/etc/veil", "generated", "caddy", "Caddyfile") {
		t.Fatalf("artifact = %+v ok=%v", artifact, ok)
	}
	for _, want := range []string{"panel.example.com", "handle_path /panel-secret/*", "reverse_proxy 127.0.0.1:2096"} {
		if !strings.Contains(artifact.Body, want) {
			t.Fatalf("Panel Caddy access material missing %q:\n%s", want, artifact.Body)
		}
	}

	intent := access.ApplyIntent([]Inbound{{Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true}})
	if len(intent.Configs) != 1 || intent.Configs[0] != "/etc/veil/generated/caddy/Caddyfile" || len(intent.Actions) != 1 || intent.Actions[0] != "reload veil-naive.service" || len(intent.Runtimes) != 1 || intent.Runtimes[0] != "veil-naive.service" {
		t.Fatalf("intent material = %+v", intent)
	}
	if len(intent.Errors) != 1 || !strings.Contains(intent.Errors[0], "panel caddy access uses 443/tcp") {
		t.Fatalf("intent errors = %+v", intent.Errors)
	}
}

func TestPanelAccessSkipsNonCaddyMaterial(t *testing.T) {
	artifact, ok, err := NewPanelAccess(Settings{PanelAccess: "local", PanelListen: "127.0.0.1:2096"}).GeneratedConfig(NewGeneratedConfigPaths("/etc/veil"))
	if err != nil || ok || artifact.Path != "" {
		t.Fatalf("non-caddy artifact = %+v ok=%v err=%v", artifact, ok, err)
	}
}
