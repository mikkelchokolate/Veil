package cli

import (
	"os"
	"strings"
	"testing"
)

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
