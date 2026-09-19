package cli

import (
	"os"
	"strings"
	"testing"
)

// Regression for #354: /etc/veil/panel TLS is shared with the protocol units
// (User=veil-proxy), so the package migration path must group it veil-proxy —
// the same contract as /etc/veil/generated and /etc/veil/tls.
func TestPostinstallGroupsPanelTLSForProxyReaders(t *testing.T) {
	body, err := os.ReadFile("../../packaging/scripts/postinstall.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(string(body), "\r\n", "\n")
	if !strings.Contains(script, "chown -R root:veil-proxy /etc/veil/panel") {
		t.Fatalf("postinstall.sh must group /etc/veil/panel as veil-proxy:\n%s", script)
	}
	if strings.Contains(script, "chown -R root:veil /etc/veil/panel") {
		t.Fatal("postinstall.sh must not regroup /etc/veil/panel back to the panel-only veil group")
	}
}

func TestAPKUpgradeRunsHardenedConfigurationHook(t *testing.T) {
	body, err := os.ReadFile("../../packaging/nfpm.yaml")
	if err != nil {
		t.Fatal(err)
	}
	config := strings.ReplaceAll(string(body), "\r\n", "\n")
	for _, want := range []string{
		"apk:\n  scripts:\n    postupgrade: packaging/scripts/postinstall.sh",
		"postinstall: packaging/scripts/postinstall.sh",
	} {
		if !strings.Contains(config, want) {
			t.Fatalf("nfpm configuration missing APK upgrade hardening contract %q:\n%s", want, config)
		}
	}
}
