package runtimeinstall

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRuntimeProbeMaliciousHelper(t *testing.T) {
	exact := "-test.run=^TestRuntimeProbeMaliciousHelper$"
	found := false
	for _, arg := range os.Args {
		if arg == exact {
			found = true
			break
		}
	}
	if !found {
		t.Skip("helper is only executed inside the runtime sandbox")
	}
	for _, path := range []string{"/etc/veil/state.key", "/etc/shadow"} {
		if body, err := os.ReadFile(path); err == nil {
			t.Fatalf("read host secret %s: %q", path, body)
		}
	}
	if value := os.Getenv("VEIL_MALICIOUS_SECRET"); value != "" {
		t.Fatalf("inherited host environment: %q", value)
	}
	for fd := 3; fd < 64; fd++ {
		link, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", fd))
		if err == nil && strings.Contains(link, "veil-host-fd-secret") {
			t.Fatalf("inherited host descriptor %d -> %s", fd, link)
		}
	}
	connection, err := net.DialTimeout("tcp", "127.0.0.1:47891", 100*time.Millisecond)
	if err == nil {
		_ = connection.Close()
		t.Fatal("connected to a host-network endpoint")
	}
	fmt.Println("sandbox-ok")
}

func TestMaliciousRuntimeProbeCannotReadHostSecretsEnvironmentDescriptorsOrNetwork(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("systemd sandbox integration requires root")
	}
	if _, err := os.Stat("/run/systemd/private"); err != nil {
		t.Skip("systemd manager is unavailable")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:47891")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	secretFile, err := os.CreateTemp("", "veil-host-fd-secret-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(secretFile.Name())
	defer secretFile.Close()
	t.Setenv("VEIL_MALICIOUS_SECRET", "must-not-cross-sandbox")
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	output, err := runSandboxedVersionProbe(ctx, binary, []string{"-test.run=^TestRuntimeProbeMaliciousHelper$", "-test.v"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "sandbox-ok") {
		t.Fatalf("malicious probe did not complete expected checks: %q", output)
	}
}
