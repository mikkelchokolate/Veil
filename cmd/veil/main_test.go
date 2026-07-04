package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestRunHelp(t *testing.T) {
	origArgs := os.Args
	os.Args = []string{"veil"}
	defer func() { os.Args = origArgs }()

	code := run()
	if code != 0 {
		t.Fatalf("expected exit code 0 for default (help) command, got: %d", code)
	}
}

func TestRunHelpFlag(t *testing.T) {
	origArgs := os.Args
	os.Args = []string{"veil", "--help"}
	defer func() { os.Args = origArgs }()

	code := run()
	if code != 0 {
		t.Fatalf("expected exit code 0 for --help, got: %d", code)
	}
}

func TestRunVersion(t *testing.T) {
	origVersion := version
	version = "test-version"
	defer func() { version = origVersion }()

	origArgs := os.Args
	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stdout = w
	os.Args = []string{"veil", "version"}
	defer func() {
		os.Args = origArgs
		os.Stdout = origStdout
	}()

	code := run()
	w.Close()

	if code != 0 {
		t.Fatalf("expected exit code 0 for version command, got: %d", code)
	}

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := strings.TrimSpace(buf.String())
	if output != version {
		t.Fatalf("expected version output %q, got: %q", version, output)
	}
}

func TestRunInvalidCommand(t *testing.T) {
	origArgs := os.Args
	origStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stderr = w
	os.Args = []string{"veil", "nonexistent"}
	defer func() {
		os.Args = origArgs
		os.Stderr = origStderr
	}()

	code := run()
	w.Close()

	if code != 1 {
		t.Fatalf("expected exit code 1 for invalid command, got: %d", code)
	}

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()
	if !strings.Contains(output, "veil:") {
		t.Errorf("expected stderr output to contain 'veil:', got: %q", output)
	}
	if !strings.Contains(output, "nonexistent") {
		t.Errorf("expected stderr output to mention 'nonexistent', got: %q", output)
	}
}

func TestRunInvalidCommandWithoutSubcommand(t *testing.T) {
	origArgs := os.Args
	origStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stderr = w
	os.Args = []string{"veil", "--unknown-flag"}
	defer func() {
		os.Args = origArgs
		os.Stderr = origStderr
	}()

	code := run()
	w.Close()

	if code != 1 {
		t.Fatalf("expected exit code 1 for unknown flag, got: %d", code)
	}

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()
	if !strings.Contains(output, "veil:") {
		t.Errorf("expected stderr output to contain 'veil:', got: %q", output)
	}
}

func TestRunDoctorDispatch(t *testing.T) {
	origArgs := os.Args
	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stdout = w
	os.Args = []string{"veil", "doctor"}
	defer func() {
		os.Args = origArgs
		os.Stdout = origStdout
	}()

	code := run()
	w.Close()

	if code != 0 {
		t.Fatalf("expected exit code 0 for doctor command, got: %d", code)
	}

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()
	if output == "" {
		t.Error("expected doctor command to produce output")
	}
}

func TestHandleErrorPrintsToStderr(t *testing.T) {
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
