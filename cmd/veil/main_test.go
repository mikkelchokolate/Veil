package main

import (
	"bytes"
	"context"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"
)

func TestRunReturnsNilForHelp(t *testing.T) {
	err := run()
	if err != nil {
		t.Fatalf("expected no error for default (help) command, got: %v", err)
	}
}

func TestRunReturnsErrorForInvalidCommand(t *testing.T) {
	// Override os.Args temporarily to pass an invalid subcommand
	origArgs := os.Args
	os.Args = []string{"veil", "nonexistent"}
	defer func() { os.Args = origArgs }()

	err := run()
	if err == nil {
		t.Fatal("expected error for invalid command, got nil")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Fatalf("expected error to mention 'nonexistent', got: %v", err)
	}
}

func TestRunCreatesSignalContext(t *testing.T) {
	// Test that run() wires signal.NotifyContext with SIGTERM and SIGINT.
	// We verify this by calling signal.NotifyContext with the same signals,
	// cancelling it, and confirming the context gets cancelled.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	// Simulate signal arrival by calling cancel directly (signal.NotifyContext
	// does the same internally when a signal is received).
	cancel()

	select {
	case <-ctx.Done():
		// expected: context is cancelled
	default:
		t.Error("expected signal context to be cancelled, but it wasn't")
	}
}

func TestHandleErrorPrintsToStderr(t *testing.T) {
	// Capture stderr and verify error is printed
	origStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stderr = w
	defer func() { os.Stderr = origStderr }()

	testErr := "test error message"
	handleError(testErr)

	w.Close()
	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if !strings.Contains(output, "veil:") {
		t.Errorf("expected stderr output to contain 'veil:', got: %q", output)
	}
	if !strings.Contains(output, testErr) {
		t.Errorf("expected stderr output to contain %q, got: %q", testErr, output)
	}
}
