package runtimeinstall

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
		"--property=SystemCallFilter=@system-service",
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
		if strings.HasPrefix(option, "--property=SystemCallFilter=") && option != "--property=SystemCallFilter=@system-service" {
			t.Fatalf("sandbox broadens the system-service syscall allowlist: %q", option)
		}
	}
}

func TestRuntimeVersionProbeDependencyTargetsRejectEscape(t *testing.T) {
	for _, dependency := range []string{"../outside/loader", "/../outside/loader", "/"} {
		t.Run(dependency, func(t *testing.T) {
			parent := t.TempDir()
			root := filepath.Join(parent, "sandbox")
			outside := filepath.Join(parent, "outside")
			for _, directory := range []string{root, outside} {
				if err := os.Mkdir(directory, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.Stat(outside)
			if err != nil {
				t.Fatal(err)
			}
			if err := prepareRuntimeProbeDependencyTargets(root, []string{dependency}); err == nil {
				t.Fatal("accepted dependency outside the sandbox")
			}
			after, err := os.Stat(outside)
			if err != nil {
				t.Fatal(err)
			}
			if before.Mode() != after.Mode() {
				t.Fatalf("outside directory permissions changed: %v -> %v", before.Mode(), after.Mode())
			}
			for _, directory := range []string{root, outside} {
				entries, err := os.ReadDir(directory)
				if err != nil || len(entries) != 0 {
					t.Fatalf("dependency rejection changed %s: entries=%v error=%v", directory, entries, err)
				}
			}
		})
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

func TestRuntimeVersionProbeFromPrivateTmpHelper(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("helper sandbox integration requires root")
	}
	if _, err := os.Stat("/run/systemd/private"); err != nil {
		t.Skip("systemd manager is unavailable")
	}
	shared, err := os.MkdirTemp("/var/lib", "veil-runtime-probe-helper-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(shared)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.Open(executable)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	binary := filepath.Join(shared, "runtime-probe-test")
	target, err := os.OpenFile(binary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	_, copyErr := io.Copy(target, source)
	closeErr := target.Close()
	if copyErr != nil {
		t.Fatal(copyErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/systemd-run", "--quiet", "--pipe", "--wait", "--collect",
		"--property=Type=exec", "--property=PrivateTmp=yes", "--property=ProtectSystem=strict",
		"--property=ProtectHome=yes", "--property=UMask=0077", "--property=ReadWritePaths="+shared,
		"--", binary, "-test.run=^TestRuntimeVersionProbePrivateTmpHelper$", "-test.v")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("probe from hardened helper failed: %v\n%s", err, output)
	}
}

func TestRuntimeVersionProbePrivateTmpHelper(t *testing.T) {
	found := false
	for _, arg := range os.Args {
		if arg == "-test.run=^TestRuntimeVersionProbePrivateTmpHelper$" {
			found = true
		}
	}
	if !found {
		t.Skip("only executed inside the PrivateTmp helper")
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	if _, err := runSandboxedVersionProbe(ctx, binary, []string{"-test.run=^$"}); err != nil {
		t.Fatal(err)
	}
}
