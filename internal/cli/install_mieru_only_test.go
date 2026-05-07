package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestInstallDryRunMieruOnlyDoesNotRequireDomainEmailOrProxyPort(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"install", "--stack", "mieru", "--dry-run"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Mieru-only dry-run should not require domain/email/port: %v\n%s", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "Stack: mieru") || !strings.Contains(got, "Mieru asset:") || strings.Contains(got, "Generated Caddyfile") {
		t.Fatalf("unexpected Mieru-only output:\n%s", got)
	}
}
