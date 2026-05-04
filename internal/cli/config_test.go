package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigValidateValidState(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	validState := `{
  "settings": {
    "panelListen": "127.0.0.1:2096",
    "stack": "both",
    "mode": "server"
  },
  "inbounds": [
    {"name": "naive", "protocol": "naiveproxy", "transport": "tcp", "port": 443, "enabled": true}
  ],
  "routingRules": [
    {"name": "default", "match": "geoip:private", "outbound": "direct", "enabled": true}
  ],
  "warp": {"enabled": false}
}
`
	if err := os.WriteFile(statePath, []byte(validState), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"config", "validate", "--state", statePath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\noutput: %s", err, out.String())
	}
	if !strings.Contains(out.String(), "Valid.") {
		t.Fatalf("expected Valid., got: %s", out.String())
	}
}

func TestConfigValidateMissingSettings(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	if err := os.WriteFile(statePath, []byte(`{"inbounds":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"config", "validate", "--state", statePath})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing settings")
	}
	if !strings.Contains(out.String(), "settings is missing") {
		t.Fatalf("expected 'settings is missing' in output, got: %s", out.String())
	}
}

func TestConfigValidateInvalidStack(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	badState := `{"settings":{"panelListen":"127.0.0.1:2096","stack":"invalid","mode":"server"},"inbounds":[],"routingRules":[]}`
	if err := os.WriteFile(statePath, []byte(badState), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"config", "validate", "--state", statePath})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid stack")
	}
	if !strings.Contains(err.Error(), "validation failed") {
		t.Fatalf("expected validation failed, got: %v", err)
	}
}

func TestConfigValidateInvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	if err := os.WriteFile(statePath, []byte(`not json`), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"config", "validate", "--state", statePath})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "invalid JSON") {
		t.Fatalf("expected invalid JSON, got: %v", err)
	}
}

func TestConfigValidateFileNotFound(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"config", "validate", "--state", "/nonexistent/file.json"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestConfigValidateDuplicatePorts(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	state := `{
  "settings": {"panelListen": "127.0.0.1:2096", "stack": "both", "mode": "server"},
  "inbounds": [
    {"name": "a", "protocol": "naiveproxy", "transport": "tcp", "port": 443, "enabled": true},
    {"name": "b", "protocol": "naiveproxy", "transport": "tcp", "port": 443, "enabled": true}
  ],
  "routingRules": []
}`
	if err := os.WriteFile(statePath, []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"config", "validate", "--state", statePath})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for duplicate port")
	}
	if !strings.Contains(out.String(), "duplicate transport/port") {
		t.Fatalf("expected duplicate transport/port error, got: %s", out.String())
	}
}

func TestConfigCommandRegistered(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"config", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := out.String()
	for _, want := range []string{"validate", "Manage Veil configuration"} {
		if !strings.Contains(got, want) {
			t.Errorf("help missing %q:\n%s", want, got)
		}
	}
}
