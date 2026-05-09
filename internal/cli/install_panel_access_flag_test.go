package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestInstallPanelOnlyHonorsDirectPanelAccessFlag(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"install", "--profile", "ru-recommended", "--panel-access", "direct", "--panel-port", "2096", "--dry-run"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("install dry-run: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "Panel access: https://0.0.0.0:2096/") {
		t.Fatalf("missing direct Panel access output:\n%s", out.String())
	}
}

func TestInstallRejectsInvalidPanelAccessFlag(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"install", "--profile", "ru-recommended", "--panel-access", "public", "--dry-run"})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "panel access must be direct, local, or caddy") {
		t.Fatalf("err = %v\n%s", err, out.String())
	}
}
