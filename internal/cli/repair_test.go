package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRepairCommandRejectsInvalidProfile(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"repair", "--profile", "invalid-profile", "--domain", "example.com", "--email", "admin@example.com", "--port", "443", "--dry-run"})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error for invalid profile, got nil\noutput: %s", out.String())
	}
	if !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("expected 'not implemented' error, got: %v", err)
	}
}

func TestRepairCommandRejectsMissingDomain(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"repair", "--profile", "ru-recommended", "--email", "admin@example.com", "--port", "443", "--dry-run"})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error for missing domain, got nil\noutput: %s", out.String())
	}
	if !strings.Contains(err.Error(), "--domain is required") {
		t.Fatalf("expected '--domain is required' error, got: %v", err)
	}
}

func TestRepairCommandRejectsMissingEmail(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"repair", "--profile", "ru-recommended", "--domain", "example.com", "--port", "443", "--dry-run"})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error for missing email, got nil\noutput: %s", out.String())
	}
	if !strings.Contains(err.Error(), "--email is required") {
		t.Fatalf("expected '--email is required' error, got: %v", err)
	}
}

func TestRepairCommandRejectsInvalidPort(t *testing.T) {
	tests := []struct {
		name string
		port string
	}{
		{"zero port", "0"},
		{"negative port", "-1"},
		{"port above max", "99999"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewRootCommand("test")
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs([]string{"repair", "--profile", "ru-recommended", "--domain", "example.com", "--email", "admin@example.com", "--port", tt.port, "--dry-run"})

			err := cmd.Execute()
			if err == nil {
				t.Fatalf("expected error for invalid port %s, got nil\noutput: %s", tt.port, out.String())
			}
			if !strings.Contains(err.Error(), "--port is required") {
				t.Fatalf("expected '--port is required' error for port %s, got: %v", tt.port, err)
			}
		})
	}
}
