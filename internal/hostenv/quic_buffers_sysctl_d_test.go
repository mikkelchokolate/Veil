package hostenv

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedSysctlDir writes sysctl files into a temporary directory and points the
// QUIC tuning at it.
func seedSysctlDir(t *testing.T, files map[string]string) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	quicSysctlDirs = []string{dir}
	quicSysctlConf = filepath.Join(dir, "no-sysctl.conf")
}

// TestApplyQUICUDPBuffersKeepsATunedSysctlDFile locks #1492: 99-veil-quic.conf
// sorts after the operator's 90-network-tuning.conf, so the live caps already
// read Veil's own 16 MiB clamp. The tuning must recover the operator value from
// the sysctl files, persist it, and raise the live caps to it.
func TestApplyQUICUDPBuffersKeepsATunedSysctlDFile(t *testing.T) {
	fixture := newQUICFloorFixture(t, map[string]string{
		quicRMEMSysctlPath: "16777216",
		quicWMEMSysctlPath: "16777216",
	})
	seedSysctlDir(t, map[string]string{
		"90-network-tuning.conf": "net.core.rmem_max = 67108864\nnet.core.wmem_max = 33554432\n",
		"99-veil-quic.conf":      "net.core.rmem_max = 16777216\nnet.core.wmem_max = 16777216\n",
	})

	if err := ApplyQUICUDPBuffers(); err != nil {
		t.Fatalf("ApplyQUICUDPBuffers: %v", err)
	}
	for _, want := range []string{"net.core.rmem_max = 67108864", "net.core.wmem_max = 33554432"} {
		if !strings.Contains(fixture.written, want) {
			t.Fatalf("rewritten drop-in lost the operator value %q:\n%s", want, fixture.written)
		}
	}
	joined := ""
	for _, command := range fixture.commands {
		joined += strings.Join(command, " ") + "\n"
	}
	for _, want := range []string{"net.core.rmem_max=67108864", "net.core.wmem_max=33554432"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("live caps were not raised to the operator value %q:\n%s", want, joined)
		}
	}
}

// TestApplyQUICUDPBuffersReadsEverySysctlDForm covers the file syntax the scan
// has to understand: `key=value`, the `-` ignore-failure prefix, comments, and
// a value spread over several files (the highest wins).
func TestApplyQUICUDPBuffersReadsEverySysctlDForm(t *testing.T) {
	fixture := newQUICFloorFixture(t, map[string]string{
		quicRMEMSysctlPath: "212992",
		quicWMEMSysctlPath: "212992",
	})
	seedSysctlDir(t, map[string]string{
		"10-a.conf": "# net.core.rmem_max = 1\n; net.core.wmem_max = 2\n-net.core.rmem_max=33554432\n",
		"50-b.conf": "net.core.rmem_max = 67108864\nnet.core.wmem_max=67108864\n",
		"70-c.conf": "net.core.rmem_max = 4194304\n",
	})

	if err := ApplyQUICUDPBuffers(); err != nil {
		t.Fatalf("ApplyQUICUDPBuffers: %v", err)
	}
	for _, want := range []string{"net.core.rmem_max = 67108864", "net.core.wmem_max = 67108864"} {
		if !strings.Contains(fixture.written, want) {
			t.Fatalf("drop-in missing the highest explicit value %q:\n%s", want, fixture.written)
		}
	}
}

// TestApplyQUICUDPBuffersIgnoresUnparsableSysctlValues keeps the floor when the
// files carry something that is not a number.
func TestApplyQUICUDPBuffersIgnoresUnparsableSysctlValues(t *testing.T) {
	fixture := newQUICFloorFixture(t, map[string]string{
		quicRMEMSysctlPath: "212992",
		quicWMEMSysctlPath: "212992",
	})
	seedSysctlDir(t, map[string]string{
		"90-tuning.conf": fmt.Sprintf("net.core.rmem_max = %s\nnet.core.wmem_max = unlimited\n", "16777216k"),
	})

	if err := ApplyQUICUDPBuffers(); err != nil {
		t.Fatalf("ApplyQUICUDPBuffers: %v", err)
	}
	for _, want := range []string{"net.core.rmem_max = 16777216", "net.core.wmem_max = 16777216"} {
		if !strings.Contains(fixture.written, want) {
			t.Fatalf("drop-in lost the floor %q:\n%s", want, fixture.written)
		}
	}
}

// TestApplyQUICUDPBuffersHonoursSysctlConf covers the file systemd-sysctl
// applies last: an operator tuning in /etc/sysctl.conf must survive the same
// way a sysctl.d file does (#1492).
func TestApplyQUICUDPBuffersHonoursSysctlConf(t *testing.T) {
	fixture := newQUICFloorFixture(t, map[string]string{
		quicRMEMSysctlPath: "16777216",
		quicWMEMSysctlPath: "16777216",
	})
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sysctl.conf"), []byte("net.core.rmem_max = 134217728\nnet.core.wmem_max = 134217728\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	quicSysctlDirs = []string{filepath.Join(dir, "sysctl.d")}
	quicSysctlConf = filepath.Join(dir, "sysctl.conf")

	if err := ApplyQUICUDPBuffers(); err != nil {
		t.Fatalf("ApplyQUICUDPBuffers: %v", err)
	}
	for _, want := range []string{"net.core.rmem_max = 134217728", "net.core.wmem_max = 134217728"} {
		if !strings.Contains(fixture.written, want) {
			t.Fatalf("drop-in lost the /etc/sysctl.conf value %q:\n%s", want, fixture.written)
		}
	}
}
