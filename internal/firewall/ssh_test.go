package firewall

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDetectSSHPortsDefaultsTo22WhenConfigMissing(t *testing.T) {
	dir := t.TempDir()
	restore := overrideSSHConfig(t, []string{filepath.Join(dir, "missing")}, filepath.Join(dir, "empty", "*.conf"))
	defer restore()
	if ports := DetectSSHPorts(); !reflect.DeepEqual(ports, []int{22}) {
		t.Fatalf("DetectSSHPorts() = %v, want [22]", ports)
	}
}

func TestDetectSSHPortsReadsCustomPort(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sshd_config")
	if err := os.WriteFile(path, []byte("#Port 22\nPort 2222\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	restore := overrideSSHConfig(t, []string{path}, filepath.Join(dir, "missing", "*.conf"))
	defer restore()
	if ports := DetectSSHPorts(); !reflect.DeepEqual(ports, []int{2222}) {
		t.Fatalf("DetectSSHPorts() = %v, want [2222]", ports)
	}
}

func TestDetectSSHPortsReadsDropInConfigs(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "sshd_config")
	dropInDir := filepath.Join(dir, "sshd_config.d")
	if err := os.Mkdir(dropInDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(main, []byte("Port 22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dropInDir, "custom.conf"), []byte("Port 2200\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	restore := overrideSSHConfig(t, []string{main}, filepath.Join(dropInDir, "*.conf"))
	defer restore()
	if ports := DetectSSHPorts(); !reflect.DeepEqual(ports, []int{22, 2200}) {
		t.Fatalf("DetectSSHPorts() = %v, want [22 2200]", ports)
	}
}

// #355: sshd commonly binds ports only via ListenAddress host:port. Missing
// them plans a UFW SSH allow for 22 while sshd listens elsewhere — lockout.
func TestDetectSSHPortsReadsListenAddressPorts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sshd_config")
	config := strings.Join([]string{
		"ListenAddress 0.0.0.0:2222",
		"ListenAddress [2001:db8::1]:2200",
		"ListenAddress [::]:2200",  // duplicate port, deduped
		"ListenAddress 192.0.2.10", // bare address binds the Port directives
		"Port 22",
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	restore := overrideSSHConfig(t, []string{path}, filepath.Join(dir, "missing", "*.conf"))
	defer restore()
	if ports := DetectSSHPorts(); !reflect.DeepEqual(ports, []int{2222, 2200, 22}) {
		t.Fatalf("DetectSSHPorts() = %v, want [2222 2200 22]", ports)
	}
}

func TestDetectSSHPortsListenAddressOnlyNoDefault22(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sshd_config")
	if err := os.WriteFile(path, []byte("ListenAddress 10.0.0.5:2222\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	restore := overrideSSHConfig(t, []string{path}, filepath.Join(dir, "missing", "*.conf"))
	defer restore()
	if ports := DetectSSHPorts(); !reflect.DeepEqual(ports, []int{2222}) {
		t.Fatalf("DetectSSHPorts() = %v, want [2222]", ports)
	}
}

func TestDetectSSHPortsBareListenAddressStillDefaultsTo22(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sshd_config")
	// A bare ListenAddress carries no port; with no Port directive sshd binds
	// the default 22.
	if err := os.WriteFile(path, []byte("ListenAddress 10.0.0.5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	restore := overrideSSHConfig(t, []string{path}, filepath.Join(dir, "missing", "*.conf"))
	defer restore()
	if ports := DetectSSHPorts(); !reflect.DeepEqual(ports, []int{22}) {
		t.Fatalf("DetectSSHPorts() = %v, want [22]", ports)
	}
}

func TestDetectSSHPortsSkipsInvalidListenAddressPorts(t *testing.T) {
	for _, spec := range []string{"0.0.0.0:0", "0.0.0.0:65536", "0.0.0.0:abc", "host:"} {
		if port, ok := sshListenAddressPort(spec); ok {
			t.Fatalf("sshListenAddressPort(%q) = %d, true; want false", spec, port)
		}
	}
	for spec, want := range map[string]int{
		"0.0.0.0:2222":       2222,
		"[2001:db8::1]:2200": 2200,
		"example.com:443":    443,
	} {
		if port, ok := sshListenAddressPort(spec); !ok || port != want {
			t.Fatalf("sshListenAddressPort(%q) = %d, %v; want %d, true", spec, port, ok, want)
		}
	}
}

func overrideSSHConfig(t *testing.T, paths []string, glob string) func() {
	t.Helper()
	origPaths, origGlob := sshConfigPaths, sshConfigGlob
	sshConfigPaths = paths
	sshConfigGlob = glob
	return func() {
		sshConfigPaths = origPaths
		sshConfigGlob = origGlob
	}
}
