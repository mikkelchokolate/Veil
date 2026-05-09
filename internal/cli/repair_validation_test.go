package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var _repair_validation_deps = []any{
	bytes.Buffer{}, os.ReadFile, filepath.Join, strings.Contains, testing.T{},
}

func TestRepairWorkflowOptionsDoNotExposeDeprecatedProtocolInstallInputs(t *testing.T) {
	optionsType := reflect.TypeOf(repairWorkflowOptions{})
	for _, field := range []string{"Stack", "Domain", "Email", "SharedPort"} {
		if _, ok := optionsType.FieldByName(field); ok {
			t.Fatalf("repairWorkflowOptions should not expose deprecated protocol install field %s", field)
		}
	}
}

func TestRepairHelpHidesLegacyStackDomainEmailAndPortFlags(t *testing.T) {
	cmd := newRepairCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("help: %v", err)
	}
	for _, unwanted := range []string{"--stack", "--domain", "--email", "--port"} {
		if strings.Contains(out.String(), unwanted) {
			t.Fatalf("repair help should hide legacy flag %q:\n%s", unwanted, out.String())
		}
	}
}

func TestRepairCommandRejectsInvalidProfile(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"repair", "--profile", "invalid-profile", "--dry-run"})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error for invalid profile, got nil\noutput: %s", out.String())
	}
	if !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("expected 'not implemented' error, got: %v", err)
	}
}

func TestRepairCommandDefaultsToRURecommendedPanelRepair(t *testing.T) {
	dir := t.TempDir()
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"repair", "--etc-dir", filepath.Join(dir, "etc", "veil"), "--var-dir", filepath.Join(dir, "var", "lib", "veil"), "--dry-run"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("default repair should work: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "Veil repair plan") {
		t.Fatalf("repair output missing plan:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "/etc/systemd/system/veil.service") {
		t.Fatalf("default repair should include Panel systemd unit repair:\n%s", out.String())
	}
}

func TestRepairCommandRejectsRemovedStackFlag(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"repair", "--profile", "ru-recommended", "--stack", "mieru", "--dry-run"})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "unknown flag: --stack") {
		t.Fatalf("expected --stack to be removed, got %v\n%s", err, out.String())
	}
}

func TestRepairCommandDoesNotRequireDeprecatedDomainEmailOrPort(t *testing.T) {
	dir := t.TempDir()
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"repair", "--profile", "ru-recommended", "--etc-dir", filepath.Join(dir, "etc", "veil"), "--var-dir", filepath.Join(dir, "var", "lib", "veil"), "--systemd-dir", filepath.Join(dir, "systemd"), "--dry-run"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Panel repair should not require deprecated domain/email/port: %v\n%s", err, out.String())
	}
	for _, want := range []string{"Veil repair plan", "veil.service"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("repair output missing %q:\n%s", want, out.String())
		}
	}
}
