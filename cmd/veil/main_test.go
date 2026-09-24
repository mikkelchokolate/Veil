package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// captureStdout swaps os.Stdout for a pipe, runs fn, and returns whatever was
// written — the cobra default help path writes to stdout, so a bare exit-code
// assertion would pass even if run() printed nothing at all.
func captureStdout(t *testing.T, fn func() int) (int, string) {
	t.Helper()
	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = origStdout }()

	code := fn()
	if err := w.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}
	return code, buf.String()
}

// helpOutputTokens are the tokens that prove the help path printed the real
// command catalog rather than an empty page or an error placeholder.
var helpOutputTokens = []string{
	"Usage:",
	"Commands:",
	"veil serve",
	"veil install",
	"veil doctor",
	"veil version",
}

func TestRunHelp(t *testing.T) {
	origArgs := os.Args
	os.Args = []string{"veil"}
	defer func() { os.Args = origArgs }()

	code, output := captureStdout(t, run)
	if code != 0 {
		t.Fatalf("expected exit code 0 for default (help) command, got: %d", code)
	}
	for _, want := range helpOutputTokens {
		if !strings.Contains(output, want) {
			t.Errorf("help output missing %q:\n%s", want, output)
		}
	}
}

func TestRunHelpFlag(t *testing.T) {
	origArgs := os.Args
	os.Args = []string{"veil", "--help"}
	defer func() { os.Args = origArgs }()

	code, output := captureStdout(t, run)
	if code != 0 {
		t.Fatalf("expected exit code 0 for --help, got: %d", code)
	}
	for _, want := range helpOutputTokens {
		if !strings.Contains(output, want) {
			t.Errorf("--help output missing %q:\n%s", want, output)
		}
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
	os.Args = []string{"veil", "doctor"}
	defer func() { os.Args = origArgs }()

	code, output := captureStdout(t, run)
	if code != 0 {
		t.Fatalf("expected exit code 0 for doctor command, got: %d", code)
	}

	// Lock the readiness summary shape: a doctor that printed nothing, or a
	// stubbed "not implemented" placeholder, must fail here.
	for _, want := range []string{
		"Veil doctor",
		"Version:",
		"Runtime:",
		"Ready:",
		"Required commands:",
		"systemctl",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("doctor output missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "not implemented") {
		t.Errorf("doctor output still contains a stub placeholder:\n%s", output)
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
