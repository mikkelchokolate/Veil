package firewall

import (
	"os"
	"path/filepath"
	"reflect"
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

// sshd may bind a port only via ListenAddress host:port with no Port
// directive at all; the installer must open that port in UFW or the operator
// is locked out (audit #355).
func TestDetectSSHPortsReadsListenAddressPort(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sshd_config")
	if err := os.WriteFile(path, []byte("ListenAddress 0.0.0.0:2222\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	restore := overrideSSHConfig(t, []string{path}, filepath.Join(dir, "missing", "*.conf"))
	defer restore()
	if ports := DetectSSHPorts(); !reflect.DeepEqual(ports, []int{2222}) {
		t.Fatalf("DetectSSHPorts() = %v, want [2222]", ports)
	}
}

func TestDetectSSHPortsUnionsPortAndListenAddress(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sshd_config")
	body := "Port 22\nListenAddress 192.0.2.1:2222\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	restore := overrideSSHConfig(t, []string{path}, filepath.Join(dir, "missing", "*.conf"))
	defer restore()
	if ports := DetectSSHPorts(); !reflect.DeepEqual(ports, []int{22, 2222}) {
		t.Fatalf("DetectSSHPorts() = %v, want [22 2222]", ports)
	}
}

func TestDetectSSHPortsReadsBracketedIPv6ListenAddress(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sshd_config")
	if err := os.WriteFile(path, []byte("ListenAddress [2001:db8::1]:2200\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	restore := overrideSSHConfig(t, []string{path}, filepath.Join(dir, "missing", "*.conf"))
	defer restore()
	if ports := DetectSSHPorts(); !reflect.DeepEqual(ports, []int{2200}) {
		t.Fatalf("DetectSSHPorts() = %v, want [2200]", ports)
	}
}

// A ListenAddress without an explicit port carries no port of its own —
// sshd falls back to Port/default 22 — and an unbracketed IPv6 literal is an
// address, not address:port.
func TestDetectSSHPortsIgnoresListenAddressWithoutPort(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sshd_config")
	body := "ListenAddress 0.0.0.0\nListenAddress ::1\nListenAddress [2001:db8::5]\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	restore := overrideSSHConfig(t, []string{path}, filepath.Join(dir, "missing", "*.conf"))
	defer restore()
	if ports := DetectSSHPorts(); !reflect.DeepEqual(ports, []int{22}) {
		t.Fatalf("DetectSSHPorts() = %v, want [22]", ports)
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
