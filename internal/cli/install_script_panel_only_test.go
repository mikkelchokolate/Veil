package cli

import (
	"os"
	"strings"
	"testing"
)

func TestCurlInstallScriptDefaultsToPanelOnlyAndSupportsPanelAccess(t *testing.T) {
	body, err := os.ReadFile("../../scripts/install-privileged.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(body)
	for _, want := range []string{
		`PANEL_ACCESS=""`,
		`--panel-access`,
		`prompted interactively when omitted`,
		`Veil install only installs Panel; configure protocols as Panel Inbounds`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("install.sh missing %q", want)
		}
	}
	if strings.Contains(script, `STACK=`) {
		t.Fatalf("install.sh should not carry legacy stack state:\n%s", script)
	}
}
