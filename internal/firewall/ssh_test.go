package firewall

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
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
	// Socket unit dirs are cleared as well: on a host that really uses
	// socket-activated SSH, the live unit's ListenStream ports would leak
	// into these fixtures.
	return overrideSSHDetection(t, paths, glob, sshConfigBaseDir, nil)
}

func overrideSSHDetection(t *testing.T, paths []string, glob, baseDir string, socketDirs []string) func() {
	t.Helper()
	origPaths, origGlob, origBase, origDirs := sshConfigPaths, sshConfigGlob, sshConfigBaseDir, sshSocketUnitDirs
	sshConfigPaths = paths
	sshConfigGlob = glob
	sshConfigBaseDir = baseDir
	sshSocketUnitDirs = socketDirs
	return func() {
		sshConfigPaths = origPaths
		sshConfigGlob = origGlob
		sshConfigBaseDir = origBase
		sshSocketUnitDirs = origDirs
	}
}

func writeSSHFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// #616: the real SSH port may live only behind an Include in the main
// sshd_config — the parallel drop-in glob never sees it.
func TestDetectSSHPortsFollowsMainConfigInclude(t *testing.T) {
	dir := t.TempDir()
	included := filepath.Join(dir, "ports.conf")
	writeSSHFile(t, included, "Port 2222\n")
	main := filepath.Join(dir, "sshd_config")
	writeSSHFile(t, main, "Include "+included+"\n")
	restore := overrideSSHDetection(t, []string{main}, filepath.Join(dir, "missing", "*.conf"), dir, nil)
	defer restore()
	if ports := DetectSSHPorts(); !reflect.DeepEqual(ports, []int{2222}) {
		t.Fatalf("DetectSSHPorts() = %v, want [2222] via main Include", ports)
	}
}

// #616: nested Include — the drop-in only contains another Include, so a
// non-recursive reader sees no port at all and falls back to 22.
func TestDetectSSHPortsFollowsNestedDropInInclude(t *testing.T) {
	dir := t.TempDir()
	dropInDir := filepath.Join(dir, "sshd_config.d")
	main := filepath.Join(dir, "sshd_config")
	writeSSHFile(t, main, "Include "+filepath.Join(dropInDir, "*.conf")+"\n")
	writeSSHFile(t, filepath.Join(dropInDir, "00-ports.conf"), "Include "+filepath.Join(dir, "sshd_ports.conf")+"\n")
	writeSSHFile(t, filepath.Join(dir, "sshd_ports.conf"), "Port 2222\n")
	// The drop-in glob is deliberately absent: discovery must come from the
	// Include chain alone.
	restore := overrideSSHDetection(t, []string{main}, "", dir, nil)
	defer restore()
	if ports := DetectSSHPorts(); !reflect.DeepEqual(ports, []int{2222}) {
		t.Fatalf("DetectSSHPorts() = %v, want [2222] via nested Include", ports)
	}
}

// #616: OpenSSH resolves a non-absolute Include under /etc/ssh, not next to
// the including file — sshConfigBaseDir stands in for /etc/ssh here.
func TestDetectSSHPortsResolvesRelativeIncludeUnderBaseDir(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "etc-ssh")
	writeSSHFile(t, filepath.Join(base, "ports-extra.conf"), "Port 2200\n")
	main := filepath.Join(dir, "sshd_config")
	writeSSHFile(t, main, "Port 22\nInclude ports-extra.conf\n")
	restore := overrideSSHDetection(t, []string{main}, "", base, nil)
	defer restore()
	if ports := DetectSSHPorts(); !reflect.DeepEqual(ports, []int{22, 2200}) {
		t.Fatalf("DetectSSHPorts() = %v, want [22 2200]", ports)
	}
}

// #616: Include cycles must terminate — the visited set prevents infinite
// recursion, and both files' ports are still collected.
func TestDetectSSHPortsIncludeCycleTerminates(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.conf")
	b := filepath.Join(dir, "b.conf")
	writeSSHFile(t, a, "Port 2222\nInclude "+b+"\n")
	writeSSHFile(t, b, "Port 2200\nInclude "+a+"\n")
	restore := overrideSSHDetection(t, []string{a}, "", dir, nil)
	defer restore()
	if ports := DetectSSHPorts(); !reflect.DeepEqual(ports, []int{2222, 2200}) {
		t.Fatalf("DetectSSHPorts() = %v, want [2222 2200]", ports)
	}
}

// #616: the include depth cap keeps a self-nesting chain bounded; files
// beyond the cap are simply not read.
func TestDetectSSHPortsIncludeDepthIsBounded(t *testing.T) {
	dir := t.TempDir()
	chain := make([]string, 0, sshIncludeMaxDepth+4)
	for i := 0; i < sshIncludeMaxDepth+4; i++ {
		chain = append(chain, filepath.Join(dir, "chain-"+strconv.Itoa(i)+".conf"))
	}
	for i, path := range chain {
		body := "Port " + strconv.Itoa(4000+i) + "\n"
		if i+1 < len(chain) {
			body += "Include " + chain[i+1] + "\n"
		}
		writeSSHFile(t, path, body)
	}
	restore := overrideSSHDetection(t, []string{chain[0]}, "", dir, nil)
	defer restore()
	ports := DetectSSHPorts()
	if len(ports) != sshIncludeMaxDepth+1 {
		t.Fatalf("DetectSSHPorts() = %v, want %d ports (depth cap %d)", ports, sshIncludeMaxDepth+1, sshIncludeMaxDepth)
	}
	for i, port := range ports {
		if port != 4000+i {
			t.Fatalf("DetectSSHPorts() = %v, want ports 4000..%d", ports, 4000+sshIncludeMaxDepth)
		}
	}
}

// #616: an Include pointing at nothing must be skipped quietly, not fail
// detection or panic.
func TestDetectSSHPortsSkipsMissingInclude(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "sshd_config")
	writeSSHFile(t, main, "Include "+filepath.Join(dir, "missing", "*.conf")+"\nInclude "+filepath.Join(dir, "gone.conf")+"\nPort 2222\n")
	restore := overrideSSHDetection(t, []string{main}, "", dir, nil)
	defer restore()
	if ports := DetectSSHPorts(); !reflect.DeepEqual(ports, []int{2222}) {
		t.Fatalf("DetectSSHPorts() = %v, want [2222]", ports)
	}
}

// OpenSSH also accepts the "keyword=value" form — a missed Port= is the same
// lockout class as a missed Include.
func TestDetectSSHPortsReadsEqualsSeparatedPort(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "sshd_config")
	writeSSHFile(t, main, "Port=2222\n")
	restore := overrideSSHDetection(t, []string{main}, "", dir, nil)
	defer restore()
	if ports := DetectSSHPorts(); !reflect.DeepEqual(ports, []int{2222}) {
		t.Fatalf("DetectSSHPorts() = %v, want [2222]", ports)
	}
}

// #633: socket-activated SSH carries the real port only in the socket unit —
// with no Port in sshd_config the union must still find it.
func TestDetectSSHPortsReadsSocketListenStream(t *testing.T) {
	dir := t.TempDir()
	unitDir := filepath.Join(dir, "systemd")
	writeSSHFile(t, filepath.Join(unitDir, "ssh.socket"), "[Socket]\nListenStream=2222\n")
	main := filepath.Join(dir, "sshd_config")
	writeSSHFile(t, main, "# no Port directives\n")
	restore := overrideSSHDetection(t, []string{main}, "", dir, []string{unitDir})
	defer restore()
	if ports := DetectSSHPorts(); !reflect.DeepEqual(ports, []int{2222}) {
		t.Fatalf("DetectSSHPorts() = %v, want [2222] from ssh.socket", ports)
	}
}

// #633: socket drop-ins override/extend the packaged unit — the port may
// live only in ssh.socket.d/*.conf.
func TestDetectSSHPortsReadsSocketDropIn(t *testing.T) {
	dir := t.TempDir()
	unitDir := filepath.Join(dir, "systemd")
	writeSSHFile(t, filepath.Join(unitDir, "ssh.socket"), "[Socket]\nListenStream=22\n")
	writeSSHFile(t, filepath.Join(unitDir, "ssh.socket.d", "50-custom.conf"), "[Socket]\nListenStream=\nListenStream=0.0.0.0:2222\n")
	main := filepath.Join(dir, "sshd_config")
	writeSSHFile(t, main, "# empty\n")
	restore := overrideSSHDetection(t, []string{main}, "", dir, []string{unitDir})
	defer restore()
	if ports := DetectSSHPorts(); !reflect.DeepEqual(ports, []int{22, 2222}) {
		t.Fatalf("DetectSSHPorts() = %v, want [22 2222]", ports)
	}
}

// #633: ListenStream outside [Socket] (or in a non-socket unit section) is
// not a socket bind and must not contribute ports.
func TestDetectSSHPortsIgnoresListenStreamOutsideSocketSection(t *testing.T) {
	dir := t.TempDir()
	unitDir := filepath.Join(dir, "systemd")
	writeSSHFile(t, filepath.Join(unitDir, "ssh.socket"), strings.Join([]string{
		"[Unit]",
		"ListenStream=1111",
		"[Socket]",
		"ListenStream=2222",
		"[Install]",
		"ListenStream=3333",
	}, "\n")+"\n")
	main := filepath.Join(dir, "sshd_config")
	writeSSHFile(t, main, "# empty\n")
	restore := overrideSSHDetection(t, []string{main}, "", dir, []string{unitDir})
	defer restore()
	if ports := DetectSSHPorts(); !reflect.DeepEqual(ports, []int{2222}) {
		t.Fatalf("DetectSSHPorts() = %v, want [2222] only", ports)
	}
}

// #633: config and socket sources union — sshd_config Port plus a socket
// ListenStream both stay reachable.
func TestDetectSSHPortsUnionsConfigAndSocketPorts(t *testing.T) {
	dir := t.TempDir()
	unitDir := filepath.Join(dir, "systemd")
	writeSSHFile(t, filepath.Join(unitDir, "ssh.socket"), "[Socket]\nListenStream=[::]:2200\n")
	main := filepath.Join(dir, "sshd_config")
	writeSSHFile(t, main, "Port 22\n")
	restore := overrideSSHDetection(t, []string{main}, "", dir, []string{unitDir})
	defer restore()
	if ports := DetectSSHPorts(); !reflect.DeepEqual(ports, []int{22, 2200}) {
		t.Fatalf("DetectSSHPorts() = %v, want [22 2200]", ports)
	}
}

func TestSSHSocketListenPort(t *testing.T) {
	for value, want := range map[string]int{
		"2222":          2222,
		"0.0.0.0:2200":  2200,
		"[::]:2300":     2300,
		"[::1]:22":      22,
		"ssh":           22,
		"192.0.2.1:443": 443,
	} {
		if port, ok := sshSocketListenPort(value); !ok || port != want {
			t.Fatalf("sshSocketListenPort(%q) = %d, %v; want %d, true", value, port, ok, want)
		}
	}
	for _, value := range []string{"", "/run/ssh.socket", "0.0.0.0:0", "65536", "not-a-port", "0.0.0.0:abc"} {
		if port, ok := sshSocketListenPort(value); ok {
			t.Fatalf("sshSocketListenPort(%q) = %d, true; want false", value, port)
		}
	}
}
