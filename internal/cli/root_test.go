package cli

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestDoctorCommandPrintsReadinessSummary(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"doctor"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{
		"Veil doctor",
		"Version: test",
		"Runtime:",
		"Required commands:",
		"caddy:",
		"hysteria:",
		"sing-box:",
		"ufw:",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "not implemented") {
		t.Fatalf("doctor output should not be a placeholder:\n%s", got)
	}
}

func TestDoctorCommandPrintsJSONReadinessSummary(t *testing.T) {
	oldLookPath := commandLookPath
	commandLookPath = func(name string) (string, error) {
		if name == "systemctl" {
			return "", errCommandNotFound
		}
		return "/usr/bin/" + name, nil
	}
	t.Cleanup(func() { commandLookPath = oldLookPath })

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"doctor", "--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{
		`"version":"test"`,
		`"runtime"`,
		`"ready":false`,
		`"commands"`,
		`"name":"caddy"`,
		`"name":"hysteria"`,
		`"name":"sing-box"`,
		`"name":"ufw"`,
		`"name":"systemctl","error":"command not found","present":false`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("doctor JSON output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Veil doctor") || strings.Contains(got, "not implemented") {
		t.Fatalf("doctor JSON output should be machine-readable only:\n%s", got)
	}
}

func TestDoctorCommandReportsOverallReadiness(t *testing.T) {
	oldLookPath := commandLookPath
	commandLookPath = func(name string) (string, error) {
		if name == "hysteria" {
			return "", errCommandNotFound
		}
		return "/usr/bin/" + name, nil
	}
	t.Cleanup(func() { commandLookPath = oldLookPath })

	summary := buildDoctorSummary("test")
	if summary.Ready {
		t.Fatalf("expected doctor summary to be not ready when a required command is missing: %+v", summary)
	}

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"doctor"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "Ready: no") {
		t.Fatalf("doctor output missing readiness verdict:\n%s", got)
	}
	if !strings.Contains(got, "hysteria: missing (command not found)") {
		t.Fatalf("doctor output missing command error detail:\n%s", got)
	}
}

func TestDoctorMissingUfwIsWarningNotError(t *testing.T) {
	oldLookPath := commandLookPath
	commandLookPath = func(name string) (string, error) {
		if name == "ufw" {
			return "", errCommandNotFound
		}
		return "/usr/bin/" + name, nil
	}
	t.Cleanup(func() { commandLookPath = oldLookPath })

	summary := buildDoctorSummary("test")
	if !summary.Ready {
		t.Fatalf("expected doctor to be ready when only ufw (optional) is missing, got Ready=false")
	}

	t.Run("human", func(t *testing.T) {
		cmd := NewRootCommand("test")
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs([]string{"doctor"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := out.String()
		if !strings.Contains(got, "Ready: yes") {
			t.Fatalf("expected Ready: yes, got:\n%s", got)
		}
		if !strings.Contains(got, "ufw: missing (warning)") {
			t.Fatalf("expected ufw warning, got:\n%s", got)
		}
		if !strings.Contains(got, "Optional commands:") {
			t.Fatalf("expected Optional commands section, got:\n%s", got)
		}
	})

	t.Run("json", func(t *testing.T) {
		cmd := NewRootCommand("test")
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs([]string{"doctor", "--json"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := out.String()
		if !strings.Contains(got, `"ready":true`) {
			t.Fatalf("expected ready:true in JSON, got:\n%s", got)
		}
		if !strings.Contains(got, `"name":"ufw","error":"command not found","present":false,"optional":true`) {
			t.Fatalf("expected ufw with optional:true, got:\n%s", got)
		}
	})
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"v0.1.0", "v0.1.0", 0},
		{"v0.1.0", "v0.2.0", -1},
		{"v0.2.0", "v0.1.0", 1},
		{"v0.1.0", "v1.0.0", -1},
		{"v1.0.0", "v0.1.0", 1},
		{"v0.1.0", "v0.1.1", -1},
		{"0.1.0", "0.1.0", 0},
		{"0.1.0", "v0.2.0", -1},
		{"1.0.0", "1.0.0", 0},
		{"2.0.0", "1.9.9", 1},
		{"v0.1.0-alpha", "v0.1.0", 0}, // non-numeric parts treated as 0
		{"dev", "v0.1.0", -1},          // non-semver shorter than semver
	}
	for _, tt := range tests {
		got := compareVersions(tt.a, tt.b)
		switch {
		case tt.want < 0 && got >= 0:
			t.Errorf("compareVersions(%q, %q) = %d, want < 0", tt.a, tt.b, got)
		case tt.want > 0 && got <= 0:
			t.Errorf("compareVersions(%q, %q) = %d, want > 0", tt.a, tt.b, got)
		case tt.want == 0 && got != 0:
			t.Errorf("compareVersions(%q, %q) = %d, want 0", tt.a, tt.b, got)
		}
	}
}

func TestVersionCheckFlagRegistered(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"version", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "--check") {
		t.Errorf("version --help missing --check flag:\n%s", got)
	}
}

func TestVersionCommandPrintsVersion(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"version"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "test") {
		t.Errorf("version output missing version, got: %s", got)
	}
}

func TestVersionCheckPrintsErrorMessageOnNetworkFailure(t *testing.T) {
	oldClient := versionCheckClient
	versionCheckClient = &http.Client{Timeout: 1 * time.Nanosecond}
	t.Cleanup(func() { versionCheckClient = oldClient })

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"version", "--check"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error on network timeout")
	}
	if !strings.Contains(err.Error(), "update check failed") {
		t.Errorf("expected 'update check failed' error, got: %v", err)
	}
}
