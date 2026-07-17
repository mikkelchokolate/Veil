package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestInstallDryRunPanelOnlyDoesNotRequireDomainEmailOrProxyPort(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"install", "--profile", "ru-recommended", "--dry-run"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("panel-only dry-run should not require domain/email/port: %v\n%s", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "Panel access: https://") || strings.Contains(got, "Generated Caddy JSON") || strings.Contains(got, "Generated Hysteria2 server.yaml") {
		t.Fatalf("unexpected panel-only dry-run output:\n%s", got)
	}
}
