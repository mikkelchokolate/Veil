package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestInstallInteractivePromptsForAccessModeAndRandomPanelPort(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetIn(strings.NewReader("local\nrandom\n"))
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"install", "--profile", "ru-recommended", "--interactive", "--dry-run"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{
		"Panel access mode",
		"Panel port mode",
		"Install scope: Panel",
		"Panel port:",
		"(random)",
		"Panel access: https://127.0.0.1:",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"Customize panel port?", "Domain for Veil/ACME:", "ACME email:", "Shared proxy port:"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("default interactive install should not prompt %q:\n%s", unwanted, got)
		}
	}
}

func TestInstallInteractiveAcceptsCustomPanelPort(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetIn(strings.NewReader("direct\ncustom\n3096\n"))
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"install", "--profile", "ru-recommended", "--interactive", "--dry-run"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "Panel port: 3096 (user selected)") || !strings.Contains(got, "Panel access: https://0.0.0.0:3096/") {
		t.Fatalf("expected custom panel port output:\n%s", got)
	}
}

func TestInstallInteractiveRejectsInvalidDomainAndRepromptsForPanelCaddy(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetIn(strings.NewReader("random\nnot-a-domain\nvalid.example.com\nadmin@example.com\n"))
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"install", "--profile", "ru-recommended", "--panel-access", "caddy", "--interactive", "--dry-run"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "Domain: valid.example.com") {
		t.Fatalf("expected valid domain in output:\n%s", got)
	}
	if strings.Contains(got, "Domain: not-a-domain") {
		t.Fatalf("expected invalid domain to be rejected, got:\n%s", got)
	}
}

func TestInstallInteractiveDoesNotPromptForSharedProxyPort(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetIn(strings.NewReader("local\nrandom\n"))
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"install", "--profile", "ru-recommended", "--interactive", "--dry-run"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out.String())
	}
	if strings.Contains(out.String(), "Shared proxy port:") || strings.Contains(out.String(), "NaiveProxy TCP port:") {
		t.Fatalf("interactive Panel install must not configure proxy port:\n%s", out.String())
	}
}

func TestInstallInteractiveRejectsInvalidPanelPortAndReprompts(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetIn(strings.NewReader("local\ncustom\n0\n99999\nxyz\n2096\n"))
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"install", "--profile", "ru-recommended", "--interactive", "--dry-run"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "Panel port: 2096 (user selected)") {
		t.Fatalf("expected custom panel port 2096 in output:\n%s", got)
	}
}
