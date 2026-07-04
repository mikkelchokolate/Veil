package service

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func writeStatusHelper(t *testing.T, script string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "status-helper-*.sh")
	if err != nil {
		t.Fatalf("create temp helper: %v", err)
	}
	if _, err := f.WriteString("#!/bin/sh\n" + script); err != nil {
		t.Fatalf("write helper: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close helper: %v", err)
	}
	if err := os.Chmod(f.Name(), 0o755); err != nil {
		t.Fatalf("chmod helper: %v", err)
	}
	return f.Name()
}

func TestReadSystemdServiceStatusParsesOutput(t *testing.T) {
	helper := writeStatusHelper(t, "printf 'LoadState=loaded\\nActiveState=active\\nSubState=running\\n'")
	old := execCommandContext
	defer func() { execCommandContext = old }()
	execCommandContext = func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		return exec.CommandContext(ctx, helper)
	}

	status := ReadSystemdServiceStatus("veil.service")
	if status.Unit != "veil.service" {
		t.Fatalf("Unit = %q, want veil.service", status.Unit)
	}
	if status.LoadState != "loaded" || status.ActiveState != "active" || status.SubState != "running" {
		t.Fatalf("status = %+v", status)
	}
	if status.Error != "" {
		t.Fatalf("unexpected error: %q", status.Error)
	}
}

func TestReadSystemdServiceStatusUsesOutputAsError(t *testing.T) {
	helper := writeStatusHelper(t, "printf 'failed to connect\\n'; exit 1")
	old := execCommandContext
	defer func() { execCommandContext = old }()
	execCommandContext = func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		return exec.CommandContext(ctx, helper)
	}

	status := ReadSystemdServiceStatus("veil.service")
	if status.LoadState != "unknown" {
		t.Fatalf("LoadState = %q, want unknown", status.LoadState)
	}
	if !strings.Contains(status.Error, "failed to connect") {
		t.Fatalf("Error = %q, want to contain 'failed to connect'", status.Error)
	}
}

func TestReadSystemdServiceStatusFallsBackToErrorMessage(t *testing.T) {
	helper := writeStatusHelper(t, "exit 1")
	old := execCommandContext
	defer func() { execCommandContext = old }()
	execCommandContext = func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		return exec.CommandContext(ctx, helper)
	}

	status := ReadSystemdServiceStatus("veil.service")
	if status.Error == "" {
		t.Fatal("expected non-empty error")
	}
}
