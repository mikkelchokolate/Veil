package runtimeinstall

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestRuntimeVersionProbeSandboxRejectsHostEscapeCapabilities(t *testing.T) {
	binary := "/var/lib/veil/runtime/staged/attacker-runtime"
	args := runtimeVersionProbeArgs(binary, []string{"--version", "--property=PrivateNetwork=no"})
	required := []string{
		"--property=NoNewPrivileges=yes",
		"--property=PrivateNetwork=yes",
		"--property=PrivateDevices=yes",
		"--property=ProtectSystem=strict",
		"--property=ProtectHome=yes",
		"--property=CapabilityBoundingSet=",
		"--property=RestrictAddressFamilies=AF_UNIX",
		"--property=SystemCallArchitectures=native",
		"--property=MemoryMax=128M",
		"--property=TasksMax=32",
	}
	for _, property := range required {
		if !slices.Contains(args, property) {
			t.Errorf("sandbox is missing %q", property)
		}
	}
	separator := slices.Index(args, "--")
	if separator < 0 || separator+1 >= len(args) || args[separator+1] != "/probe/runtime" {
		t.Fatalf("runtime command is not isolated after --: %q", args)
	}
	bind := "--property=BindReadOnlyPaths=" + binary + ":/probe/runtime"
	if !slices.Contains(args[:separator], bind) {
		t.Fatalf("staged runtime is not bound into the systemd sandbox: %q", args)
	}
	for _, option := range args[:separator] {
		if strings.Contains(option, "PrivateNetwork=no") {
			t.Fatalf("attacker-controlled runtime argument escaped into systemd-run options: %q", args)
		}
	}
}

func TestRuntimeVersionProbeBubblewrapFallbackIsReadOnlyAndNetworkIsolated(t *testing.T) {
	args := bubblewrapVersionProbeArgs("/tmp/staged-runtime", []string{"version"})
	joined := strings.Join(args, " ")
	for _, required := range []string{
		"--unshare-all",
		"--tmpfs /",
		"--tmpfs /tmp",
		"--ro-bind /tmp/staged-runtime /probe/runtime",
		"--clearenv",
		"-- /probe/runtime version",
	} {
		if !strings.Contains(joined, required) {
			t.Fatalf("bubblewrap probe missing %q: %s", required, joined)
		}
	}
	if strings.Contains(joined, "--share-net") || strings.Contains(joined, "--bind / /") || strings.Contains(joined, "--ro-bind / /") {
		t.Fatalf("bubblewrap probe exposes host capabilities: %s", joined)
	}
}

func TestRuntimeVersionProbeExecutesProtectedHomeBinaryThroughSystemdBind(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("systemd sandbox integration requires root")
	}
	if _, err := os.Stat("/run/systemd/private"); err != nil {
		t.Skip("systemd manager is unavailable")
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := runSandboxedVersionProbe(ctx, binary, []string{"-test.run=^$"}); err != nil {
		t.Fatal(err)
	}
}
