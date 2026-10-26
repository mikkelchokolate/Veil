package repair

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/api"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/secrets"
)

// TestRepairKeepsTheStateRootInVeilEnv locks #1444: the panel material built by
// repair used to receive only EtcDir, so the regenerated veil.env lost
// VEIL_VAR_DIR, VEIL_STATE_PATH, VEIL_APPLY_ROOT and VEIL_AUTO_TLS_DIR — which
// `veil install` writes — even when the operator passed the right --var-dir.
// The dropped keys then showed up as drift on every later repair. Repairs now
// forward VarDir; this test keeps it that way.
func TestRepairKeepsTheStateRootInVeilEnv(t *testing.T) {
	dir := t.TempDir()
	varDir := filepath.Join(dir, "srv-veil")
	etcDir := filepath.Join(dir, "etc", "veil")
	for _, path := range []string{varDir, etcDir} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
	}
	key, err := secrets.LoadOrCreateKey(filepath.Join(etcDir, "state.key"))
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	cipher, err := secrets.NewCipher(*key)
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	statePath := filepath.Join(varDir, "state.json")
	snapshot := managementstate.BuildSnapshot(managementstate.SnapshotInput{
		Settings: api.Settings{PanelListen: "127.0.0.1:2096", Mode: "server", PanelAccess: "local"},
		Inbounds: []api.Inbound{},
		Rules:    []api.RoutingRule{},
		Warp:     api.WarpConfig{Endpoint: "engage.cloudflareclient.com:2408"},
	})
	if err := managementstate.NewStore(statePath, cipher).Save(snapshot); err != nil {
		t.Fatalf("save state: %v", err)
	}

	plan, err := buildRepairPlanFromOptions(Options{
		Profile:    "ru-recommended",
		EtcDir:     etcDir,
		VarDir:     varDir,
		SystemdDir: filepath.Join(dir, "systemd"),
	})
	if err != nil {
		t.Fatalf("buildRepairPlanFromOptions: %v", err)
	}
	env := repairActionContent(plan, "veil.env")
	if env == "" {
		t.Fatalf("repair planned no veil.env:\n%s", plan.Summary())
	}
	for _, want := range []string{
		"VEIL_VAR_DIR=" + filepath.ToSlash(varDir),
		"VEIL_STATE_PATH=" + filepath.ToSlash(filepath.Join(varDir, "state.json")),
		"VEIL_APPLY_ROOT=" + filepath.ToSlash(filepath.Join(varDir, "staging")),
		"VEIL_AUTO_TLS_DIR=" + filepath.ToSlash(filepath.Join(varDir, "autocert")),
	} {
		if !strings.Contains(env, want) {
			t.Fatalf("regenerated veil.env lost %q:\n%s", want, env)
		}
	}
	// The packaged state root must not leak into a custom --var-dir install.
	if strings.Contains(env, "VEIL_VAR_DIR=/var/lib/veil") {
		t.Fatalf("regenerated veil.env points at the packaged state root:\n%s", env)
	}
}

// TestRepairKeepsTheRecordedSystemdDir locks the persistence round-trip from
// #1484: a repair on a custom --systemd-dir install must keep
// VEIL_SYSTEMD_DIR in the regenerated veil.env, otherwise the next repair (and
// the rotate-key preflight) falls back to the packaged unit directory.
func TestRepairKeepsTheRecordedSystemdDir(t *testing.T) {
	etcDir := t.TempDir()
	varDir := t.TempDir()
	systemdDir := filepath.Join(t.TempDir(), "systemd")
	envPath := filepath.Join(etcDir, "veil.env")
	env := "VEIL_SYSTEMD_DIR=" + systemdDir + "\nVEIL_PANEL_ACCESS=local\n"
	if err := os.WriteFile(envPath, []byte(env), 0o640); err != nil {
		t.Fatal(err)
	}

	plan, err := buildRepairPlanFromOptions(Options{
		Profile: "ru-recommended", EtcDir: etcDir, VarDir: varDir, SystemdDir: systemdDir,
	})
	if err != nil {
		t.Fatalf("buildRepairPlanFromOptions: %v", err)
	}
	if got := repairPlanEnvValue(plan, "VEIL_SYSTEMD_DIR"); got != systemdDir {
		t.Fatalf("planned VEIL_SYSTEMD_DIR = %q, want %q", got, systemdDir)
	}
}
