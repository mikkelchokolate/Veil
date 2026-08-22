package hostenv

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestQUICSysctlContentSets16MiBBuffers(t *testing.T) {
	body := QUICSysctlContent()
	for _, want := range []string{"net.core.rmem_max = 16777216", "net.core.wmem_max = 16777216"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in:\n%s", want, body)
		}
	}
}

func TestApplyQUICUDPBuffersNoopWhenNotRoot(t *testing.T) {
	origEUID := quicGeteuid
	origGOOS := quicRuntimeGOOS
	t.Cleanup(func() {
		quicGeteuid = origEUID
		quicRuntimeGOOS = origGOOS
	})
	quicGeteuid = func() int { return 1000 }
	quicRuntimeGOOS = func() string { return "linux" }
	if err := ApplyQUICUDPBuffers(); err != nil {
		t.Fatal(err)
	}
}

func TestApplyQUICUDPBuffersWritesSysctlAndApplies(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "99-veil-quic.conf")
	origEUID := quicGeteuid
	origGOOS := quicRuntimeGOOS
	origMkdir := quicMkdirAll
	origWrite := quicWriteFile
	origLook := quicLookPath
	origCmd := quicCommand
	t.Cleanup(func() {
		quicGeteuid = origEUID
		quicRuntimeGOOS = origGOOS
		quicMkdirAll = origMkdir
		quicWriteFile = origWrite
		quicLookPath = origLook
		quicCommand = origCmd
	})
	quicGeteuid = func() int { return 0 }
	quicRuntimeGOOS = func() string { return "linux" }
	quicMkdirAll = func(string, os.FileMode) error { return nil }
	quicWriteFile = func(name string, data []byte, perm os.FileMode) error {
		if name != quicSysctlPath {
			t.Fatalf("write path %q", name)
		}
		return os.WriteFile(path, data, perm)
	}
	quicLookPath = func(string) (string, error) { return "/usr/sbin/sysctl", nil }
	var specs []string
	quicCommand = func(name string, args ...string) *exec.Cmd {
		specs = append(specs, strings.Join(append([]string{name}, args...), " "))
		return exec.Command("true")
	}
	if err := ApplyQUICUDPBuffers(); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != QUICSysctlContent() {
		t.Fatalf("wrote %q", body)
	}
	if len(specs) != 2 {
		t.Fatalf("sysctl invocations: %v", specs)
	}
	for _, want := range []string{
		"sysctl -w net.core.rmem_max=16777216",
		"sysctl -w net.core.wmem_max=16777216",
	} {
		found := false
		for _, got := range specs {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing %q in %v", want, specs)
		}
	}
}

func TestApplyQUICUDPBuffersNoopWhenNotLinux(t *testing.T) {
	origEUID := quicGeteuid
	origGOOS := quicRuntimeGOOS
	origWrite := quicWriteFile
	t.Cleanup(func() {
		quicGeteuid = origEUID
		quicRuntimeGOOS = origGOOS
		quicWriteFile = origWrite
	})
	quicGeteuid = func() int { return 0 }
	quicRuntimeGOOS = func() string { return "darwin" }
	quicWriteFile = func(string, []byte, os.FileMode) error {
		t.Fatal("must not write sysctl on non-linux")
		return nil
	}
	if err := ApplyQUICUDPBuffers(); err != nil {
		t.Fatal(err)
	}
}
