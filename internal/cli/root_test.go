package cli

import (
	"bytes"
	"strings"
	"testing"
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
