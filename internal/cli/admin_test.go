package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdminCommandReset(t *testing.T) {
	tempEtc := t.TempDir()
	tempVar := t.TempDir()

	cmd := newFastRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	statePath := filepath.Join(tempVar, "state.json")
	keyPath := filepath.Join(tempEtc, "state.key")

	cmd.SetArgs([]string{
		"admin", "reset",
		"--state", statePath,
		"--key-path", keyPath,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error executing admin reset: %v\n%s", err, out.String())
	}

	got := out.String()
	if !strings.Contains(got, "Admin credentials successfully reset.") {
		t.Fatalf("expected reset success output, got:\n%s", got)
	}
	if !strings.Contains(got, "Username: admin_") {
		t.Fatalf("expected Username prefix, got:\n%s", got)
	}
	if !strings.Contains(got, "Password: ") {
		t.Fatalf("expected Password print, got:\n%s", got)
	}
}

func TestAdminCommandSetAndShow(t *testing.T) {
	tempEtc := t.TempDir()
	tempVar := t.TempDir()

	statePath := filepath.Join(tempVar, "state.json")
	keyPath := filepath.Join(tempEtc, "state.key")

	// 1. Set credentials
	cmd := newFastRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"admin", "set",
		"--username", "john_doe",
		"--password", "custom-password-123",
		"--role", "admin",
		"--state", statePath,
		"--key-path", keyPath,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error executing admin set: %v\n%s", err, out.String())
	}

	got := out.String()
	if !strings.Contains(got, "User credentials successfully set.") || !strings.Contains(got, "Username: john_doe") || !strings.Contains(got, "Role: admin") {
		t.Fatalf("unexpected output on admin set:\n%s", got)
	}

	// 2. Show users list
	cmdShow := newFastRootCommand("test")
	var outShow bytes.Buffer
	cmdShow.SetOut(&outShow)
	cmdShow.SetErr(&outShow)
	cmdShow.SetArgs([]string{
		"admin", "show",
		"--state", statePath,
		"--key-path", keyPath,
	})

	if err := cmdShow.Execute(); err != nil {
		t.Fatalf("unexpected error executing admin show: %v\n%s", err, outShow.String())
	}

	gotShow := outShow.String()
	if !strings.Contains(gotShow, "Registered users:") || !strings.Contains(gotShow, "- john_doe (Role: admin)") {
		t.Fatalf("unexpected output on admin show:\n%s", gotShow)
	}
}
