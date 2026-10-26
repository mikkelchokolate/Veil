package hostenv

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// quicFloorFixture stubs the QUIC tuning seams and records what the command
// asked the host to do. The seams are package globals restored by t.Cleanup;
// these tests must stay non-parallel (no t.Parallel) because they mutate them.
type quicFloorFixture struct {
	commands [][]string
	written  string
}

func newQUICFloorFixture(t *testing.T, caps map[string]string) *quicFloorFixture {
	t.Helper()
	fixture := &quicFloorFixture{}
	original := []any{quicRuntimeGOOS, quicGeteuid, quicReadFile, quicWriteFile, quicMkdirAll, quicLookPath, quicCommand, quicGlob, quicSysctlDirs}
	t.Cleanup(func() {
		quicRuntimeGOOS = original[0].(func() string)
		quicGeteuid = original[1].(func() int)
		quicReadFile = original[2].(func(string) ([]byte, error))
		quicWriteFile = original[3].(func(string, []byte, os.FileMode) error)
		quicMkdirAll = original[4].(func(string, os.FileMode) error)
		quicLookPath = original[5].(func(string) (string, error))
		quicCommand = original[6].(func(string, ...string) *exec.Cmd)
		quicGlob = original[7].(func(string) ([]string, error))
		quicSysctlDirs = original[8].([]string)
	})
	// Hermetic by default: no sysctl.d files unless a test seeds them.
	quicSysctlDirs = nil
	quicRuntimeGOOS = func() string { return "linux" }
	quicGeteuid = func() int { return 0 }
	quicReadFile = func(path string) ([]byte, error) {
		if value, ok := caps[path]; ok {
			return []byte(value + "\n"), nil
		}
		if strings.HasPrefix(path, "/proc/") {
			return nil, errors.New("unreadable")
		}
		// Seedable files (sysctl.d) fall through to the real read.
		return os.ReadFile(path)
	}
	quicWriteFile = func(path string, body []byte, mode os.FileMode) error {
		fixture.written = string(body)
		return nil
	}
	quicMkdirAll = func(string, os.FileMode) error { return nil }
	quicLookPath = func(string) (string, error) { return "/usr/bin/sysctl", nil }
	quicCommand = func(name string, args ...string) *exec.Cmd {
		fixture.commands = append(fixture.commands, append([]string{name}, args...))
		return exec.Command("/bin/true")
	}
	return fixture
}

// TestApplyQUICUDPBuffersOnlyRaises locks #1456: a host tuned above Veil's
// 16 MiB floor must keep its limits live and after the next boot.
func TestApplyQUICUDPBuffersOnlyRaises(t *testing.T) {
	fixture := newQUICFloorFixture(t, map[string]string{
		quicRMEMSysctlPath: "67108864",
		quicWMEMSysctlPath: "33554432",
	})
	if err := ApplyQUICUDPBuffers(); err != nil {
		t.Fatalf("ApplyQUICUDPBuffers: %v", err)
	}
	if len(fixture.commands) != 0 {
		t.Fatalf("lowered live caps on an already tuned host: %v", fixture.commands)
	}
	for _, want := range []string{"net.core.rmem_max = 67108864", "net.core.wmem_max = 33554432"} {
		if !strings.Contains(fixture.written, want) {
			t.Fatalf("drop-in does not keep the higher limit %q:\n%s", want, fixture.written)
		}
	}
	if strings.Contains(fixture.written, "net.core.rmem_max = 16777216") {
		t.Fatalf("drop-in resets the RMEM cap to the floor:\n%s", fixture.written)
	}
}

// TestApplyQUICUDPBuffersRaisesBelowTheFloor keeps the original behaviour on a
// default host: both caps are raised and the drop-in carries the floor.
func TestApplyQUICUDPBuffersRaisesBelowTheFloor(t *testing.T) {
	fixture := newQUICFloorFixture(t, map[string]string{
		quicRMEMSysctlPath: "212992",
		quicWMEMSysctlPath: "212992",
	})
	if err := ApplyQUICUDPBuffers(); err != nil {
		t.Fatalf("ApplyQUICUDPBuffers: %v", err)
	}
	if len(fixture.commands) != 2 {
		t.Fatalf("commands = %v, want both caps raised", fixture.commands)
	}
	for _, want := range []string{"net.core.rmem_max = 16777216", "net.core.wmem_max = 16777216"} {
		if !strings.Contains(fixture.written, want) {
			t.Fatalf("drop-in missing floor %q:\n%s", want, fixture.written)
		}
	}
}

// TestApplyQUICUDPBuffersRaisesOnlyTheLowCap covers a mixed host: the higher
// cap is left alone, the lower one is raised.
func TestApplyQUICUDPBuffersRaisesOnlyTheLowCap(t *testing.T) {
	fixture := newQUICFloorFixture(t, map[string]string{
		quicRMEMSysctlPath: "67108864",
		quicWMEMSysctlPath: "212992",
	})
	if err := ApplyQUICUDPBuffers(); err != nil {
		t.Fatalf("ApplyQUICUDPBuffers: %v", err)
	}
	if len(fixture.commands) != 1 || !strings.Contains(strings.Join(fixture.commands[0], " "), "wmem_max") {
		t.Fatalf("commands = %v, want only the wmem raise", fixture.commands)
	}
	if !strings.Contains(fixture.written, "net.core.rmem_max = 67108864") {
		t.Fatalf("drop-in lowered the already-high rmem cap:\n%s", fixture.written)
	}
}

// TestApplyQUICUDPBuffersTreatsUnreadableCapsAsUnknown keeps the fail-safe
// direction: when /proc cannot be read the floor is applied instead of assuming
// the host is already tuned.
func TestApplyQUICUDPBuffersTreatsUnreadableCapsAsUnknown(t *testing.T) {
	fixture := newQUICFloorFixture(t, nil)
	if err := ApplyQUICUDPBuffers(); err != nil {
		t.Fatalf("ApplyQUICUDPBuffers: %v", err)
	}
	if len(fixture.commands) != 2 {
		t.Fatalf("commands = %v, want the floor applied", fixture.commands)
	}
}
