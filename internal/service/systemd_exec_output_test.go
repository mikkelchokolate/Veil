package service

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Review on #1363: flags in a batched restart must not inflate the budget.
func TestSystemdCommandTimeoutIgnoresRestartFlags(t *testing.T) {
	got := systemdCommandTimeout("systemctl", "restart", "--no-block", "--job-mode=replace", "veil.service", "veil-caddy.service")
	if want := 60 * time.Second; got != want {
		t.Fatalf("timeout = %v, want %v (flags must not count as units)", got, want)
	}
	if got := systemdCommandTimeout("systemctl", "restart", "--no-block"); got != 30*time.Second {
		t.Fatalf("flag-only restart timeout = %v, want 30s floor", got)
	}
}

func writeFailingTool(t *testing.T, script string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture")
	}
	path := filepath.Join(t.TempDir(), "fake-systemctl")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// Review on #1363: a failed batched restart must say which unit failed, so the
// runner surfaces systemctl's own diagnostics instead of a bare exit status.
func TestSystemdExecRunnerIncludesCommandOutputInError(t *testing.T) {
	tool := writeFailingTool(t, "echo 'Job for veil-caddy.service failed because the control process exited with error code.' >&2\nexit 1\n")
	err := SystemdExecRunner{}.Run(tool, "restart", "veil.service", "veil-caddy.service")
	if err == nil {
		t.Fatal("expected failure")
	}
	if !strings.Contains(err.Error(), "Job for veil-caddy.service failed") {
		t.Fatalf("error lacks systemctl diagnostics: %v", err)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
		t.Fatalf("error must still unwrap to the exit error: %v", err)
	}
}

func TestSystemdExecRunnerBoundsCommandOutputInError(t *testing.T) {
	tool := writeFailingTool(t, "i=0\nwhile [ $i -lt 2000 ]; do echo \"noise line $i padding padding padding\" >&2; i=$((i+1)); done\necho 'FINAL: veil.service failed' >&2\nexit 3\n")
	err := SystemdExecRunner{}.Run(tool, "restart", "veil.service")
	if err == nil {
		t.Fatal("expected failure")
	}
	msg := err.Error()
	if len(msg) > systemdOutputLimit+256 {
		t.Fatalf("error message is %d bytes, want bounded near %d", len(msg), systemdOutputLimit)
	}
	if !strings.Contains(msg, "FINAL: veil.service failed") {
		t.Fatalf("bounded output must keep the tail where systemctl reports the failure: %q", msg[len(msg)-200:])
	}
	if strings.Contains(msg, "noise line 0 ") {
		t.Fatal("bounded output kept the head instead of the tail")
	}
}

func TestSystemdExecRunnerSilentFailureKeepsPlainError(t *testing.T) {
	tool := writeFailingTool(t, "exit 4\n")
	err := SystemdExecRunner{}.Run(tool, "restart", "veil.service")
	if err == nil || err.Error() != "exit status 4" {
		t.Fatalf("silent failure error = %v, want plain exit status 4", err)
	}
}

// Review on #1369: an orphan that inherits the output pipes must not keep Run
// waiting after systemctl exits.
func TestSystemdExecRunnerDoesNotWaitForOrphanHoldingOutput(t *testing.T) {
	tool := writeFailingTool(t, "sleep 60 &\necho 'veil.service failed' >&2\nexit 1\n")
	start := time.Now()
	err := SystemdExecRunner{}.Run(tool, "restart", "veil.service")
	if elapsed := time.Since(start); elapsed > systemdWaitDelay+10*time.Second {
		t.Fatalf("Run took %v; an orphan holding the pipe blocked it", elapsed)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
		t.Fatalf("error = %v, want exit status 1", err)
	}
	if !strings.Contains(err.Error(), "veil.service failed") {
		t.Fatalf("error lost output written before exit: %v", err)
	}
}
