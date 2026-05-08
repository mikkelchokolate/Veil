package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/veil-panel/veil/internal/api"
	"github.com/veil-panel/veil/internal/secrets"
)

func TestBuildRepairPlanFromOptionsLoadsEncryptedPanelState(t *testing.T) {
	dir := t.TempDir()
	varDir := filepath.Join(dir, "var", "lib", "veil")
	etcDir := filepath.Join(dir, "etc", "veil")
	if err := os.MkdirAll(varDir, 0o755); err != nil {
		t.Fatalf("mkdir var dir: %v", err)
	}
	if err := os.MkdirAll(etcDir, 0o755); err != nil {
		t.Fatalf("mkdir etc dir: %v", err)
	}
	keyPath := filepath.Join(etcDir, "state.key")
	key, err := secrets.LoadOrCreateKey(keyPath)
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	cipher, err := secrets.NewCipher(*key)
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	statePath := filepath.Join(varDir, "state.json")
	snapshot := api.BuildManagementSnapshot(api.ManagementSnapshotInput{
		Settings: api.Settings{PanelListen: "127.0.0.1:2096", Stack: "panel", Mode: "server"},
		Inbounds: []api.Inbound{{Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Password: "secret-pass"}},
		Rules:    []api.RoutingRule{},
		Warp:     api.WarpConfig{Endpoint: "engage.cloudflareclient.com:2408"},
	})
	if err := api.NewStateStore(statePath, cipher).Save(snapshot); err != nil {
		t.Fatalf("save state: %v", err)
	}

	plan, err := buildRepairPlanFromOptions(repairWorkflowOptions{Profile: "ru-recommended", Stack: "panel", EtcDir: etcDir, VarDir: varDir, SystemdDir: filepath.Join(dir, "systemd")})
	if err != nil {
		t.Fatalf("buildRepairPlanFromOptions: %v", err)
	}
	if !strings.Contains(plan.Summary(), "generated/mieru/server_config.json") {
		t.Fatalf("encrypted state Mieru config not repaired:\n%s", plan.Summary())
	}
}

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
