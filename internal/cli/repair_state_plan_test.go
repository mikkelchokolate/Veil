package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/veil-panel/veil/internal/api"
	"github.com/veil-panel/veil/internal/installer"
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
		Settings: api.Settings{PanelListen: "127.0.0.1:2096", Mode: "server"},
		Inbounds: []api.Inbound{{Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Password: "secret-pass"}},
		Rules:    []api.RoutingRule{},
		Warp:     api.WarpConfig{Endpoint: "engage.cloudflareclient.com:2408"},
	})
	if err := api.NewStateStore(statePath, cipher).Save(snapshot); err != nil {
		t.Fatalf("save state: %v", err)
	}

	plan, err := buildRepairPlanFromOptions(repairWorkflowOptions{Profile: "ru-recommended", EtcDir: etcDir, VarDir: varDir, SystemdDir: filepath.Join(dir, "systemd")})
	if err != nil {
		t.Fatalf("buildRepairPlanFromOptions: %v", err)
	}
	if !strings.Contains(plan.Summary(), "generated/mieru/server_config.json") {
		t.Fatalf("encrypted state Mieru config not repaired:\n%s", plan.Summary())
	}
}

func TestBuildRepairPlanFromOptionsUsesPanelStateCaddyAccess(t *testing.T) {
	dir := t.TempDir()
	varDir := filepath.Join(dir, "var", "lib", "veil")
	statePath := filepath.Join(varDir, "state.json")
	if err := os.MkdirAll(varDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	state := `{
  "settings": {"panelListen":"127.0.0.1:2096","panelAccess":"caddy","webBasePath":"/panel-secret/","mode":"server","domain":"panel.example.com","email":"admin@example.com"},
  "inbounds": [],
  "routingRules": [],
  "warp": {"endpoint":"engage.cloudflareclient.com:2408"}
}`
	if err := os.WriteFile(statePath, []byte(state), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}

	plan, err := buildRepairPlanFromOptions(repairWorkflowOptions{Profile: "ru-recommended", EtcDir: filepath.Join(dir, "etc", "veil"), VarDir: varDir, SystemdDir: filepath.Join(dir, "systemd")})
	if err != nil {
		t.Fatalf("buildRepairPlanFromOptions: %v", err)
	}
	summary := plan.Summary()
	for _, want := range []string{"generated/caddy/Caddyfile", "veil-naive.service"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("Panel state Caddy access repair missing %q:\n%s", want, summary)
		}
	}
	caddyfile := repairActionContent(plan, "Caddyfile")
	if !strings.Contains(caddyfile, "handle_path /panel-secret/*") || !strings.Contains(caddyfile, "reverse_proxy 127.0.0.1:2096") {
		t.Fatalf("Panel state Caddyfile not repaired from settings:\n%s", caddyfile)
	}
	env := repairActionContent(plan, "veil.env")
	if !strings.Contains(env, "VEIL_PANEL_ACCESS=caddy") || strings.Contains(env, "VEIL_TLS_CERT") {
		t.Fatalf("Panel state veil.env should preserve caddy access without direct TLS:\n%s", env)
	}
}

func TestBuildRepairPlanFromOptionsUsesResolvedCaddyBinaryForNaiveRuntime(t *testing.T) {
	oldLookPath := commandLookPath
	commandLookPath = func(name string) (string, error) {
		if name == "caddy" {
			return "/usr/sbin/caddy", nil
		}
		return "", errors.New("missing")
	}
	t.Cleanup(func() { commandLookPath = oldLookPath })

	dir := t.TempDir()
	varDir := filepath.Join(dir, "var", "lib", "veil")
	statePath := filepath.Join(varDir, "state.json")
	if err := os.MkdirAll(varDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	state := `{
  "settings": {"panelListen":"127.0.0.1:2096","mode":"server","domain":"vpn.example.com","email":"admin@example.com","naiveUsername":"veil","naivePassword":"naive-secret"},
  "inbounds": [
    {"name":"naive","protocol":"naiveproxy","transport":"tcp","port":443,"enabled":true}
  ],
  "routingRules": [],
  "warp": {"endpoint":"engage.cloudflareclient.com:2408"}
}`
	if err := os.WriteFile(statePath, []byte(state), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}

	plan, err := buildRepairPlanFromOptions(repairWorkflowOptions{Profile: "ru-recommended", EtcDir: filepath.Join(dir, "etc", "veil"), VarDir: varDir, SystemdDir: filepath.Join(dir, "systemd")})
	if err != nil {
		t.Fatalf("buildRepairPlanFromOptions: %v", err)
	}
	unit := repairActionContent(plan, "veil-naive.service")
	if !strings.Contains(unit, "ExecStart=/usr/sbin/caddy run --config") || !strings.Contains(unit, "ExecReload=/usr/sbin/caddy reload --config") {
		t.Fatalf("repair should render veil-naive.service with resolved caddy path:\n%s", unit)
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
  "settings": {"panelListen":"127.0.0.1:2096","mode":"server"},
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

	plan, err := buildRepairPlanFromOptions(repairWorkflowOptions{Profile: "ru-recommended", EtcDir: filepath.Join(dir, "etc", "veil"), VarDir: varDir, SystemdDir: filepath.Join(dir, "systemd")})
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

func repairActionContent(plan installer.RepairPlan, name string) string {
	for _, action := range plan.Actions {
		if filepath.Base(action.Path) == name {
			return action.Content
		}
	}
	return ""
}
