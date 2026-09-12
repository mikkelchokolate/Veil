package hostenv

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestApplyQUICUDPBuffersReportsLiveSysctlFailures(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "99-veil-quic.conf")
	restore := stubQUICApply(t, path)
	defer restore()

	quicLookPath = func(string) (string, error) { return "/usr/sbin/sysctl", nil }
	quicCommand = func(name string, args ...string) *exec.Cmd {
		return commandExiting(1)
	}
	err := ApplyQUICUDPBuffers()
	if err == nil {
		t.Fatal("expected live sysctl failure")
	}
	if !strings.Contains(err.Error(), "net.core.rmem_max") || !strings.Contains(err.Error(), "net.core.wmem_max") {
		t.Fatalf("error=%v", err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("persistent sysctl file missing: %v", statErr)
	}
}

func TestApplyQUICUDPBuffersReportsPartialLiveSysctlFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "99-veil-quic.conf")
	restore := stubQUICApply(t, path)
	defer restore()

	quicLookPath = func(string) (string, error) { return "/usr/sbin/sysctl", nil }
	var specs []string
	quicCommand = func(name string, args ...string) *exec.Cmd {
		specs = append(specs, strings.Join(append([]string{name}, args...), " "))
		if len(specs) == 1 {
			return commandExiting(0)
		}
		return commandExiting(1)
	}
	err := ApplyQUICUDPBuffers()
	if err == nil {
		t.Fatal("expected partial live sysctl failure")
	}
	if !strings.Contains(err.Error(), "net.core.wmem_max") {
		t.Fatalf("error=%v", err)
	}
	if strings.Contains(err.Error(), "net.core.rmem_max") {
		t.Fatalf("successful rmem_max reported as failure: %v", err)
	}
}

func TestApplyQUICUDPBuffersReportsMissingSysctlBinary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "99-veil-quic.conf")
	restore := stubQUICApply(t, path)
	defer restore()

	quicLookPath = func(string) (string, error) { return "", os.ErrNotExist }
	if err := ApplyQUICUDPBuffers(); err == nil || !strings.Contains(err.Error(), "sysctl") {
		t.Fatalf("expected missing sysctl error, got %v", err)
	}
}

func stubQUICApply(t *testing.T, path string) func() {
	t.Helper()
	origEUID := quicGeteuid
	origGOOS := quicRuntimeGOOS
	origMkdir := quicMkdirAll
	origWrite := quicWriteFile
	origLook := quicLookPath
	origCmd := quicCommand
	quicGeteuid = func() int { return 0 }
	quicRuntimeGOOS = func() string { return "linux" }
	quicMkdirAll = func(string, os.FileMode) error { return nil }
	quicWriteFile = func(name string, data []byte, perm os.FileMode) error {
		if name != quicSysctlPath {
			t.Fatalf("write path %q", name)
		}
		return os.WriteFile(path, data, perm)
	}
	return func() {
		quicGeteuid = origEUID
		quicRuntimeGOOS = origGOOS
		quicMkdirAll = origMkdir
		quicWriteFile = origWrite
		quicLookPath = origLook
		quicCommand = origCmd
	}
}

func commandExiting(code int) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.Command("cmd", "/c", "exit", strconv.Itoa(code))
	}
	if code == 0 {
		return exec.Command("true")
	}
	return exec.Command("false")
}
