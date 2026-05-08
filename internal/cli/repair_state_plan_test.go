package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildRepairPlanFromOptionsUsesPanelStateMieruInbounds(t *testing.T) {
	dir := t.TempDir()
	varDir := filepath.Join(dir, "var", "lib", "veil")
	statePath := filepath.Join(varDir, "state.json")
	if err := os.MkdirAll(varDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	state := `{
  "settings": {"panelListen":"127.0.0.1:2096","stack":"panel","mode":"server"},
  "inbounds": [
    {"name":"mieru-tcp","protocol":"mieru","transport":"tcp","port":443,"enabled":true,"password":"tcp-pass"},
    {"name":"mieru-udp","protocol":"mieru","transport":"udp","port":443,"enabled":true,"password":"udp-pass"}
  ],
  "routingRules": [],
  "warp": {"endpoint":"engage.cloudflareclient.com:2408"}
}`
	if err := os.WriteFile(statePath, []byte(state), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}

	plan, err := buildRepairPlanFromOptions(repairWorkflowOptions{Profile: "ru-recommended", Stack: "panel", EtcDir: filepath.Join(dir, "etc", "veil"), VarDir: varDir, SystemdDir: filepath.Join(dir, "systemd")})
	if err != nil {
		t.Fatalf("buildRepairPlanFromOptions: %v", err)
	}
	summary := plan.Summary()
	for _, want := range []string{"generated/mieru/server_config.json", "veil-mieru.service"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("repair summary missing %q:\n%s", want, summary)
		}
	}
	for _, unwanted := range []string{"generated/caddy/Caddyfile", "shared proxy port"} {
		if strings.Contains(summary, unwanted) {
			t.Fatalf("repair summary should not include %q:\n%s", unwanted, summary)
		}
	}
}
