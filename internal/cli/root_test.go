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
		`"commands"`,
		`"name":"caddy"`,
		`"name":"hysteria"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("doctor JSON output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Veil doctor") || strings.Contains(got, "not implemented") {
		t.Fatalf("doctor JSON output should be machine-readable only:\n%s", got)
	}
}
