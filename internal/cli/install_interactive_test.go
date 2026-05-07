package cli

import (
	"bytes"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/veil-panel/veil/internal/installer"
)

var _installTestDeps_interactive = []any{
	bytes.Buffer{}, net.ParseIP, http.MethodGet, httptest.NewRecorder, os.ReadFile, filepath.Join, strings.Contains, testing.T{}, installer.StackBoth,
}

func TestInstallInteractivePromptsForDomainEmailPortAndRandomPanelPort(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetIn(strings.NewReader("example.com\nadmin@example.com\n31874\nn\n"))
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"install", "--profile", "ru-recommended", "--interactive", "--dry-run"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{
		"Domain for Veil/ACME:",
		"ACME email:",
		"Shared proxy port:",
		"Customize panel port?",
		"Domain: example.com",
		"Email: admin@example.com",
		"Panel port:",
		"(random)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

func TestInstallInteractiveAcceptsCustomPanelPort(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetIn(strings.NewReader("example.com\nadmin@example.com\n31874\ny\n2096\n"))
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"install", "--profile", "ru-recommended", "--interactive", "--dry-run"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "Panel port: 2096 (user selected)") {
		t.Fatalf("expected custom panel port output:\n%s", got)
	}
}

func TestInstallInteractiveRejectsInvalidDomainAndReprompts(t *testing.T) {
	// Pass an invalid domain (no dot), then a valid one. Verify the command succeeds.
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	// Sequence: invalid-domain-without-dot, valid-domain, email, port, n (no custom panel port)
	cmd.SetIn(strings.NewReader("not-a-domain\nvalid.example.com\nadmin@example.com\n31874\nn\n"))
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"install", "--profile", "ru-recommended", "--interactive", "--dry-run"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out.String())
	}
	got := out.String()
	// Should have reprompted and eventually used the valid domain.
	if !strings.Contains(got, "Domain: valid.example.com") {
		t.Fatalf("expected valid domain in output:\n%s", got)
	}
	// The invalid domain should NOT appear as the final domain.
	if strings.Contains(got, "Domain: not-a-domain") {
		t.Fatalf("expected invalid domain to be rejected, got:\n%s", got)
	}
}

func TestInstallInteractiveRejectsInvalidSharedPortAndReprompts(t *testing.T) {
	// Pass invalid ports (0, out-of-range, non-numeric), then a valid one.
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetIn(strings.NewReader("example.com\nadmin@example.com\n0\n99999\nabc\n31874\nn\n"))
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"install", "--profile", "ru-recommended", "--interactive", "--dry-run"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out.String())
	}
	got := out.String()
	// Should succeed with the valid port (31874 appears in output as NaiveProxy TCP port etc.)
	if !strings.Contains(got, "NaiveProxy TCP port: 31874") {
		t.Fatalf("expected valid shared port 31874 in output:\n%s", got)
	}
}

func TestInstallInteractiveRejectsInvalidPanelPortAndReprompts(t *testing.T) {
	// User chooses to customize panel port but enters invalid values, then valid.
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetIn(strings.NewReader("example.com\nadmin@example.com\n31874\ny\n0\n99999\nxyz\n2096\n"))
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
