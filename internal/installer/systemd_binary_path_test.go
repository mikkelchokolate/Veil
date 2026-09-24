package installer

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallApplyRendersVeilUnitWithSelectedBinaryPath(t *testing.T) {
	dir := t.TempDir()
	profile := RURecommendedProfile{PanelAuthToken: "secret-panel"}
	paths := ApplyPaths{EtcDir: filepath.Join(dir, "etc", "veil"), VarDir: filepath.Join(dir, "var", "lib", "veil"), SystemdDir: filepath.Join(dir, "systemd"), VeilBinary: "/opt/veil/bin/veil"}

	files, err := desiredManagedFiles(profile, paths)
	if err != nil {
		t.Fatalf("desiredFiles: %v", err)
	}
	unit := managedFileContent(files, "veil.service")
	if !strings.Contains(unit, "ExecStart=/opt/veil/bin/veil serve") {
		t.Fatalf("veil.service should use selected binary path:\n%s", unit)
	}
	// Assert the panel unit contract, not just the unit path: it runs as the
	// dedicated veil account, is bound to the helper socket, and carries no
	// capabilities of its own.
	for _, want := range []string{
		"Type=simple",
		"User=veil",
		"Group=veil",
		"Requires=veil-helper.socket",
		"Environment=VEIL_HELPER_SOCKET=/run/veil/helper.sock",
		"Restart=on-failure",
		"NoNewPrivileges=true",
		"ProtectSystem=strict",
	} {
		if !strings.Contains(unit, want+"\n") {
			t.Fatalf("veil.service missing %q:\n%s", want, unit)
		}
	}
	// The panel drops ALL capabilities — the bounding set line must be empty,
	// not merely absent (an absent line could hide a copy-paste regression).
	if !strings.Contains(unit, "CapabilityBoundingSet=\n") {
		t.Fatalf("veil.service must carry an empty CapabilityBoundingSet:\n%s", unit)
	}
}

func TestInstallApplyPropagatesCustomEtcAndVarDir(t *testing.T) {
	dir := t.TempDir()
	etcDir := filepath.Join(dir, "opt", "veil", "etc")
	varDir := filepath.Join(dir, "opt", "veil", "var")
	profile := RURecommendedProfile{PanelAuthToken: "secret-panel"}
	files, err := desiredManagedFiles(profile, ApplyPaths{EtcDir: etcDir, VarDir: varDir, SystemdDir: filepath.Join(dir, "systemd")})
	if err != nil {
		t.Fatalf("desiredFiles: %v", err)
	}
	env := managedFileContent(files, "veil.env")
	for _, want := range []string{
		"VEIL_STATE_PATH=" + filepath.ToSlash(filepath.Join(varDir, "state.json")),
		"VEIL_KEY_PATH=" + filepath.ToSlash(filepath.Join(etcDir, "state.key")),
	} {
		if !strings.Contains(env, want) {
			t.Fatalf("veil.env missing %q:\n%s", want, env)
		}
	}
	unit := managedFileContent(files, "veil.service")
	if !strings.Contains(unit, "VEIL_STATE_PATH="+filepath.ToSlash(filepath.Join(varDir, "state.json"))) {
		t.Fatalf("veil.service missing custom state path:\n%s", unit)
	}
	if !strings.Contains(unit, "ReadWritePaths="+varDir) && !strings.Contains(unit, "ReadWritePaths="+filepath.ToSlash(varDir)) {
		t.Fatalf("veil.service missing custom ReadWritePaths:\n%s", unit)
	}
}

func TestInstallApplyQuotesVeilUnitWhenBinaryPathHasSpaces(t *testing.T) {
	dir := t.TempDir()
	profile := RURecommendedProfile{PanelAuthToken: "secret-panel"}
	paths := ApplyPaths{
		EtcDir:     filepath.Join(dir, "etc", "veil"),
		VarDir:     filepath.Join(dir, "var", "lib", "veil"),
		SystemdDir: filepath.Join(dir, "systemd"),
		VeilBinary: "/opt/Veil Panel/bin/veil",
	}
	files, err := desiredManagedFiles(profile, paths)
	if err != nil {
		t.Fatalf("desiredFiles: %v", err)
	}
	unit := managedFileContent(files, "veil.service")
	if !strings.Contains(unit, `ExecStart="/opt/Veil Panel/bin/veil" serve`) {
		t.Fatalf("veil.service should quote binary path with spaces:\n%s", unit)
	}
}

func TestInstallApplyRendersCaddyUnitWithResolvedBinaryPath(t *testing.T) {
	dir := t.TempDir()
	profile := RURecommendedProfile{InstallPanelCaddy: true, PanelAuthToken: "secret-panel", CaddyJSON: "{}"}
	paths := ApplyPaths{EtcDir: filepath.Join(dir, "etc", "veil"), VarDir: filepath.Join(dir, "var", "lib", "veil"), SystemdDir: filepath.Join(dir, "systemd"), CaddyBinary: "/usr/bin/caddy"}

	files, err := desiredManagedFiles(profile, paths)
	if err != nil {
		t.Fatalf("desiredFiles: %v", err)
	}
	unit := managedFileContent(files, "veil-caddy.service")
	if !strings.Contains(unit, "ExecStart=/usr/bin/caddy run --config") || !strings.Contains(unit, "ExecReload=/usr/bin/caddy reload --config") {
		t.Fatalf("veil-caddy.service should use resolved Caddy binary path:\n%s", unit)
	}
}

func managedFileContent(files []managedFile, name string) string {
	for _, file := range files {
		if filepath.Base(file.Path) == name {
			return file.Content
		}
	}
	return ""
}
