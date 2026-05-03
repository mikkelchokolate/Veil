package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestServeCommandRejectsInvalidListenAddress(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"serve", "--listen", "bad-address"})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected invalid listen error")
	}
	if !strings.Contains(err.Error(), "listen address must be host:port") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestServeCommandRejectsInvalidPortWithAuthToken(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"serve", "--listen", "localhost:notaport", "--auth-token", "secret"})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected invalid port error when auth token is set")
	}
	if !strings.Contains(err.Error(), "invalid port") && !strings.Contains(err.Error(), "listen address") {
		t.Fatalf("expected error to mention invalid port or listen address, got: %v", err)
	}
}
