package status

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// isolatePathDiscovery clears every path-resolution input so a test exercises
// exactly the discovery channel it configures (issue #635).
func isolatePathDiscovery(t *testing.T) {
	t.Helper()
	isolateListenConfig(t)
	t.Setenv("VEIL_ETC_DIR", "")
	t.Setenv("VEIL_VAR_DIR", "")
	t.Setenv("VEIL_LIVE_ROOT", "")
	t.Setenv("VEIL_KEY_PATH", "")
	t.Setenv("VEIL_TLS_CERT", "")
	t.Setenv("VEIL_TLS_KEY", "")
	// The installed env/state/cert overrides are release seams for callers
	// that already resolved them; leave them empty here so discovery runs.
	origEnv, origState, origCert, origSystemd := installedEnvFile, installedStateFile, panelTLSCertFile, systemdSystemDir
	installedEnvFile = ""
	installedStateFile = ""
	panelTLSCertFile = ""
	systemdSystemDir = filepath.Join(t.TempDir(), "systemd")
	t.Cleanup(func() {
		installedEnvFile, installedStateFile, panelTLSCertFile, systemdSystemDir = origEnv, origState, origCert, origSystemd
	})
}

func TestStatusDiscoversEnvFileUnderCustomEtcDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("VEIL_ETC_DIR discovery targets POSIX installs")
	}
	isolatePathDiscovery(t)
	etcDir := t.TempDir()
	envPath := filepath.Join(etcDir, "veil.env")
	if err := os.WriteFile(envPath, []byte("VEIL_LISTEN=127.0.0.1:47359\nVEIL_API_TOKEN=custom-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VEIL_ETC_DIR", etcDir)

	if got := ResolveListen(""); got != "127.0.0.1:47359" {
		t.Fatalf("ResolveListen(\"\") = %q, want address from custom <etc>/veil.env", got)
	}
	if got := ResolveAuthToken(""); got != "custom-token" {
		t.Fatalf("ResolveAuthToken(\"\") = %q, want token from custom <etc>/veil.env", got)
	}
}

func TestStatusDiscoversEnvFileViaSystemdDropIn(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("systemd discovery targets POSIX installs")
	}
	isolatePathDiscovery(t)
	// A custom --etc-dir install records its EnvironmentFile in the installed
	// unit drop-in; status must follow it even with no VEIL_* env set.
	etcDir := t.TempDir()
	envPath := filepath.Join(etcDir, "veil.env")
	if err := os.WriteFile(envPath, []byte("VEIL_LISTEN=127.0.0.1:47360\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dropInDir := filepath.Join(systemdSystemDir, "veil.service.d")
	if err := os.MkdirAll(dropInDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dropIn := "[Service]\nEnvironmentFile=-" + envPath + "\n"
	if err := os.WriteFile(filepath.Join(dropInDir, "10-veil-install.conf"), []byte(dropIn), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ResolveListen(""); got != "127.0.0.1:47360" {
		t.Fatalf("ResolveListen(\"\") = %q, want address discovered via systemd drop-in", got)
	}
}

func TestStatusDiscoversStateFileFromInstalledEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("VEIL_ETC_DIR discovery targets POSIX installs")
	}
	isolatePathDiscovery(t)
	// Custom --var-dir install: the env file records VEIL_STATE_PATH and the
	// state file there carries the effective panelListen (issue #635).
	etcDir := t.TempDir()
	varDir := t.TempDir()
	statePath := filepath.Join(varDir, "state.json")
	if err := os.WriteFile(statePath, []byte(`{"settings":{"panelListen":"127.0.0.1:38413"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	envPath := filepath.Join(etcDir, "veil.env")
	if err := os.WriteFile(envPath, []byte("VEIL_STATE_PATH="+statePath+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VEIL_ETC_DIR", etcDir)

	if got := ResolveListen(""); got != "127.0.0.1:38413" {
		t.Fatalf("ResolveListen(\"\") = %q, want panelListen from custom state file", got)
	}
}

// TestEnvFileDirectiveParsesSystemdGrammar covers the EnvironmentFile= parser
// edge cases: the "-" ignore-missing marker, systemd quoting, whitespace, and
// comment/unrelated lines.
func TestEnvFileDirectiveParsesSystemdGrammar(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"plain path", "EnvironmentFile=/etc/veil/veil.env\n", "/etc/veil/veil.env"},
		{"optional marker", "EnvironmentFile=-/etc/veil/veil.env\n", "/etc/veil/veil.env"},
		{"double-quoted", "EnvironmentFile=\"/etc/veil/veil.env\"\n", "/etc/veil/veil.env"},
		{"single-quoted", "EnvironmentFile='/etc/veil/veil.env'\n", "/etc/veil/veil.env"},
		{"quoted optional marker", "EnvironmentFile=-\"/etc/veil/veil.env\"\n", "/etc/veil/veil.env"},
		{"whitespace around equals", "EnvironmentFile = /etc/veil/veil.env \n", "/etc/veil/veil.env"},
		{"skips comments and unrelated keys", "# comment\nEnvironment=FOO=bar\nEnvironmentFile=/etc/veil/veil.env\n", "/etc/veil/veil.env"},
		{"later directive wins", "EnvironmentFile=/etc/veil/first.env\nEnvironmentFile=/etc/veil/second.env\n", "/etc/veil/second.env"},
		{"empty value resets", "EnvironmentFile=/etc/veil/veil.env\nEnvironmentFile=\n", ""},
		{"missing directive", "Environment=FOO=bar\n", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := envFileDirective([]byte(tc.body)); got != tc.want {
				t.Fatalf("envFileDirective(%q) = %q, want %q", tc.body, got, tc.want)
			}
		})
	}
}

func TestStatusDiscoversPanelTLSCertUnderCustomEtcDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("VEIL_ETC_DIR discovery targets POSIX installs")
	}
	isolatePathDiscovery(t)
	etcDir := t.TempDir()
	certPath := filepath.Join(etcDir, "panel", "tls.crt")
	if err := os.MkdirAll(filepath.Dir(certPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certPath, []byte("pem"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VEIL_ETC_DIR", etcDir)
	if got := effectivePanelTLSCertFile(); got != certPath {
		t.Fatalf("panel TLS cert = %q, want %q under the custom etc dir", got, certPath)
	}
}
