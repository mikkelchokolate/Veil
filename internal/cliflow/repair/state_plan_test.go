package repair

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/api"
	"github.com/mikkelchokolate/Veil/internal/installer"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/secrets"
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
	snapshot := managementstate.BuildSnapshot(managementstate.SnapshotInput{
		Settings: api.Settings{PanelListen: "127.0.0.1:2096", Mode: "server"},
		Inbounds: []api.Inbound{{Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Password: "secret-pass"}},
		Rules:    []api.RoutingRule{},
		Warp:     api.WarpConfig{Endpoint: "engage.cloudflareclient.com:2408"},
	})
	if err := managementstate.NewStore(statePath, cipher).Save(snapshot); err != nil {
		t.Fatalf("save state: %v", err)
	}

	plan, err := buildRepairPlanFromOptions(Options{Profile: "ru-recommended", EtcDir: etcDir, VarDir: varDir, SystemdDir: filepath.Join(dir, "systemd")})
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

	plan, err := buildRepairPlanFromOptions(Options{Profile: "ru-recommended", EtcDir: filepath.Join(dir, "etc", "veil"), VarDir: varDir, SystemdDir: filepath.Join(dir, "systemd")})
	if err != nil {
		t.Fatalf("buildRepairPlanFromOptions: %v", err)
	}
	summary := plan.Summary()
	for _, want := range []string{"generated/caddy/config.json", "veil-caddy.service"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("Panel state Caddy access repair missing %q:\n%s", want, summary)
		}
	}
	caddyJSON := repairActionContent(plan, "config.json")
	if !strings.Contains(caddyJSON, "panel.example.com") || !strings.Contains(caddyJSON, "127.0.0.1:2096") || !strings.Contains(caddyJSON, "panel-secret") {
		t.Fatalf("Panel state Caddy JSON not repaired from settings:\n%s", caddyJSON)
	}
	env := repairActionContent(plan, "veil.env")
	if !strings.Contains(env, "VEIL_PANEL_ACCESS=caddy") || strings.Contains(env, "VEIL_TLS_CERT") {
		t.Fatalf("Panel state veil.env should preserve caddy access without direct TLS:\n%s", env)
	}
}

func TestBuildRepairPlanFromOptionsUsesResolvedCaddyBinaryForNaiveRuntime(t *testing.T) {
	oldLookPath := testLookPath
	testLookPath = func(name string) (string, error) {
		if name == "caddy" {
			return "/usr/sbin/caddy", nil
		}
		return "", errors.New("missing")
	}
	t.Cleanup(func() { testLookPath = oldLookPath })

	dir := t.TempDir()
	varDir := filepath.Join(dir, "var", "lib", "veil")
	statePath := filepath.Join(varDir, "state.json")
	if err := os.MkdirAll(varDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	state := `{
  "settings": {"panelListen":"127.0.0.1:2096","mode":"server","domain":"vpn.example.com","defaultAcmeEmail":"admin@example.com","naiveUsername":"veil","naivePassword":"naive-secret"},
  "inbounds": [
    {"name":"naive","protocol":"naiveproxy","transport":"tcp","port":443,"enabled":true}
  ],
  "routingRules": [],
  "warp": {"endpoint":"engage.cloudflareclient.com:2408"}
}`
	if err := os.WriteFile(statePath, []byte(state), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}

	plan, err := buildRepairPlanFromOptions(Options{Profile: "ru-recommended", EtcDir: filepath.Join(dir, "etc", "veil"), VarDir: varDir, SystemdDir: filepath.Join(dir, "systemd")})
	if err != nil {
		t.Fatalf("buildRepairPlanFromOptions: %v", err)
	}
	unit := repairActionContent(plan, "veil-caddy.service")
	if !strings.Contains(unit, "ExecStart=/usr/sbin/caddy run --config") {
		t.Fatalf("repair should render veil-caddy.service with resolved caddy path:\n%s", unit)
	}
}

// Regression for #339: on a Caddy-mode install the first-install profile stages
// a Panel-only generated/caddy/config.json placeholder. The state-derived
// render must replace it so a Hysteria2-only ACME subject survives repair.
func TestBuildRepairPlanFromOptionsKeepsHysteria2ACMEInCaddyJSON(t *testing.T) {
	dir := t.TempDir()
	varDir := filepath.Join(dir, "var", "lib", "veil")
	statePath := filepath.Join(varDir, "state.json")
	if err := os.MkdirAll(varDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	state := `{
  "settings": {"panelListen":"127.0.0.1:2096","panelAccess":"caddy","webBasePath":"/panel-secret/","mode":"server","domain":"panel.example.com","email":"admin@example.com","defaultAcmeEmail":"admin@example.com"},
  "inbounds": [
    {"name":"hy2","protocol":"hysteria2","transport":"udp","port":443,"enabled":true,"password":"hy2-pass","protocolFields":{"domain":"hy2.example.com","email":"admin@example.com"}}
  ],
  "routingRules": [],
  "warp": {"endpoint":"engage.cloudflareclient.com:2408"}
}`
	if err := os.WriteFile(statePath, []byte(state), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}

	plan, err := buildRepairPlanFromOptions(Options{Profile: "ru-recommended", EtcDir: filepath.Join(dir, "etc", "veil"), VarDir: varDir, SystemdDir: filepath.Join(dir, "systemd")})
	if err != nil {
		t.Fatalf("buildRepairPlanFromOptions: %v", err)
	}
	caddyJSON := repairActionContent(plan, "config.json")
	if !strings.Contains(caddyJSON, "hy2.example.com") {
		t.Fatalf("repair staged Panel-only Caddy JSON, dropping the Hysteria2 ACME subject:\n%s", caddyJSON)
	}
	if !strings.Contains(caddyJSON, "panel.example.com") {
		t.Fatalf("state-derived Caddy JSON lost the Panel route:\n%s", caddyJSON)
	}
}

// Regression for #339: same placeholder overwrite, exercised with a live Naive
// route that only exists in the state-derived render.
func TestBuildRepairPlanFromOptionsKeepsNaiveRouteInCaddyJSON(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires a POSIX caddy probe stub")
	}
	dir := t.TempDir()
	// naiveproxy.RenderConfig probes `caddy list-modules --json` on PATH; stub a
	// forward_proxy-capable binary so the consolidated config can render.
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeCaddy := filepath.Join(binDir, "caddy")
	stub := "#!/bin/sh\nprintf '%s' '[{\"module_name\":\"http\"},{\"module_name\":\"http.handlers.forward_proxy\"}]'\n"
	if err := os.WriteFile(fakeCaddy, []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	varDir := filepath.Join(dir, "var", "lib", "veil")
	statePath := filepath.Join(varDir, "state.json")
	if err := os.MkdirAll(varDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	state := `{
  "settings": {"panelListen":"127.0.0.1:2096","panelAccess":"caddy","webBasePath":"/panel-secret/","mode":"server","domain":"panel.example.com","email":"admin@example.com","defaultAcmeEmail":"admin@example.com","naiveUsername":"veil","naivePassword":"naive-secret"},
  "inbounds": [
    {"name":"naive","protocol":"naiveproxy","transport":"tcp","port":8443,"enabled":true,"protocolFields":{"domain":"naive.example.com","email":"admin@example.com"}}
  ],
  "routingRules": [],
  "warp": {"endpoint":"engage.cloudflareclient.com:2408"}
}`
	if err := os.WriteFile(statePath, []byte(state), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}

	plan, err := buildRepairPlanFromOptions(Options{Profile: "ru-recommended", EtcDir: filepath.Join(dir, "etc", "veil"), VarDir: varDir, SystemdDir: filepath.Join(dir, "systemd")})
	if err != nil {
		t.Fatalf("buildRepairPlanFromOptions: %v", err)
	}
	caddyJSON := repairActionContent(plan, "config.json")
	if !strings.Contains(caddyJSON, "naive.example.com") {
		t.Fatalf("repair staged Panel-only Caddy JSON, dropping the Naive route:\n%s", caddyJSON)
	}
	if !strings.Contains(caddyJSON, "panel.example.com") {
		t.Fatalf("state-derived Caddy JSON lost the Panel route:\n%s", caddyJSON)
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

	plan, err := buildRepairPlanFromOptions(Options{Profile: "ru-recommended", EtcDir: filepath.Join(dir, "etc", "veil"), VarDir: varDir, SystemdDir: filepath.Join(dir, "systemd")})
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
