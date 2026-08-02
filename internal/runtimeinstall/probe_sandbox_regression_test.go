package runtimeinstall

import (
	"slices"
	"strings"
	"testing"
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
	if separator < 0 || separator+1 >= len(args) || args[separator+1] != binary {
		t.Fatalf("runtime command is not isolated after --: %q", args)
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
		"--ro-bind / /",
		"--tmpfs /tmp",
		"--ro-bind /tmp/staged-runtime /run/veil-runtime-probe",
		"--clearenv",
		"-- /run/veil-runtime-probe version",
	} {
		if !strings.Contains(joined, required) {
			t.Fatalf("bubblewrap probe missing %q: %s", required, joined)
		}
	}
	if strings.Contains(joined, "--share-net") || strings.Contains(joined, "--bind / /") {
		t.Fatalf("bubblewrap probe exposes host capabilities: %s", joined)
	}
}
