package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestInstallCommandDefaultsToPanelOnlyProfile(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"install", "--dry-run"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("default install dry-run should be panel-only: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "Panel access:") || strings.Contains(out.String(), "Generated Caddyfile") {
		t.Fatalf("unexpected default install output:\n%s", out.String())
	}
}
