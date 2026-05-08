package cli

import (
	"os"
	"strings"
	"testing"
)

func TestCurlInstallScriptDefaultsToPanelOnlyAndSupportsPanelAccess(t *testing.T) {
	body, err := os.ReadFile("../../scripts/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(body)
	for _, want := range []string{
		`STACK="panel"`,
		`PANEL_ACCESS="local"`,
		`--panel-access`,
		`Deprecated; only panel is accepted`,
		`Veil install only installs Panel; configure protocols as Panel Inbounds.`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("install.sh missing %q", want)
		}
	}
}
