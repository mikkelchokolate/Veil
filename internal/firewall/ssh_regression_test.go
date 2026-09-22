package firewall

import (
	"path/filepath"
	"reflect"
	"testing"
)

// #673: sshd_config(5) allows keyword values in double quotes. A quoted Port
// ("Port \"2222\"" or Port="2222") used to fail Atoi and was skipped, so
// detection fell back to 22 while sshd listened elsewhere.
func TestDetectSSHPortsReadsQuotedPort(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "sshd_config")
	writeSSHFile(t, main, "Port \"2222\"\n")
	restore := overrideSSHDetection(t, []string{main}, "", dir, nil)
	defer restore()
	if ports := DetectSSHPorts(); !reflect.DeepEqual(ports, []int{2222}) {
		t.Fatalf("DetectSSHPorts() = %v, want [2222] for quoted Port", ports)
	}
}

// #673: the quoted form also applies to the keyword=value syntax.
func TestDetectSSHPortsReadsQuotedEqualsPort(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "sshd_config")
	writeSSHFile(t, main, "Port=\"2200\"\n")
	restore := overrideSSHDetection(t, []string{main}, "", dir, nil)
	defer restore()
	if ports := DetectSSHPorts(); !reflect.DeepEqual(ports, []int{2200}) {
		t.Fatalf("DetectSSHPorts() = %v, want [2200] for quoted Port=", ports)
	}
}

// #673: a quoted Include path was globbed with its quotes intact, matched
// nothing, and the ports living only in the included file were missed.
func TestDetectSSHPortsFollowsQuotedInclude(t *testing.T) {
	dir := t.TempDir()
	included := filepath.Join(dir, "ports.conf")
	writeSSHFile(t, included, "Port 2222\n")
	main := filepath.Join(dir, "sshd_config")
	writeSSHFile(t, main, "Include \""+included+"\"\n")
	restore := overrideSSHDetection(t, []string{main}, "", dir, nil)
	defer restore()
	if ports := DetectSSHPorts(); !reflect.DeepEqual(ports, []int{2222}) {
		t.Fatalf("DetectSSHPorts() = %v, want [2222] via quoted Include", ports)
	}
}

// #673: quoted Include globs must expand too — a generated drop-in directory
// referenced as Include "conf.d/*.conf" is the stock Debian layout shape.
func TestDetectSSHPortsFollowsQuotedIncludeGlob(t *testing.T) {
	dir := t.TempDir()
	dropInDir := filepath.Join(dir, "conf.d")
	writeSSHFile(t, filepath.Join(dropInDir, "10-ports.conf"), "Port 2200\n")
	main := filepath.Join(dir, "sshd_config")
	writeSSHFile(t, main, "Include \"conf.d/*.conf\"\n")
	restore := overrideSSHDetection(t, []string{main}, "", dir, nil)
	defer restore()
	if ports := DetectSSHPorts(); !reflect.DeepEqual(ports, []int{2200}) {
		t.Fatalf("DetectSSHPorts() = %v, want [2200] via quoted Include glob", ports)
	}
}

// #673: quoting exists so an argument may contain whitespace — an Include
// path with a space in a directory name must survive as a single argument.
func TestDetectSSHPortsFollowsQuotedIncludeWithSpace(t *testing.T) {
	dir := t.TempDir()
	spaced := filepath.Join(dir, "dir with space")
	writeSSHFile(t, filepath.Join(spaced, "ports.conf"), "Port 2222\n")
	main := filepath.Join(dir, "sshd_config")
	writeSSHFile(t, main, "Include \""+filepath.Join(spaced, "ports.conf")+"\"\n")
	restore := overrideSSHDetection(t, []string{main}, "", dir, nil)
	defer restore()
	if ports := DetectSSHPorts(); !reflect.DeepEqual(ports, []int{2222}) {
		t.Fatalf("DetectSSHPorts() = %v, want [2222] via quoted Include with space", ports)
	}
}

// #673: quoted ListenAddress host:port tokens contribute their port too.
func TestDetectSSHPortsReadsQuotedListenAddress(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "sshd_config")
	writeSSHFile(t, main, "ListenAddress \"0.0.0.0:2222\"\n")
	restore := overrideSSHDetection(t, []string{main}, "", dir, nil)
	defer restore()
	if ports := DetectSSHPorts(); !reflect.DeepEqual(ports, []int{2222}) {
		t.Fatalf("DetectSSHPorts() = %v, want [2222] for quoted ListenAddress", ports)
	}
}

// OpenSSH accepts multiple Port arguments on one line; every listed port is
// bound, so the detector must union them rather than read only the first.
func TestDetectSSHPortsReadsMultiplePortsOnOneLine(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "sshd_config")
	writeSSHFile(t, main, "Port 22 2222 \"2200\"\n")
	restore := overrideSSHDetection(t, []string{main}, "", dir, nil)
	defer restore()
	if ports := DetectSSHPorts(); !reflect.DeepEqual(ports, []int{22, 2222, 2200}) {
		t.Fatalf("DetectSSHPorts() = %v, want [22 2222 2200]", ports)
	}
}

// An unmatched quote runs to end of line (OpenSSH's strdelim is lenient the
// same way); the port inside must still be found rather than dropped.
func TestDetectSSHPortsReadsUnterminatedQuotedPort(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "sshd_config")
	writeSSHFile(t, main, "Port \"2222\n")
	restore := overrideSSHDetection(t, []string{main}, "", dir, nil)
	defer restore()
	if ports := DetectSSHPorts(); !reflect.DeepEqual(ports, []int{2222}) {
		t.Fatalf("DetectSSHPorts() = %v, want [2222] for unterminated quote", ports)
	}
}

func TestSSHConfigArgsStripsQuotes(t *testing.T) {
	for input, want := range map[string][]string{
		`2222`:                   {"2222"},
		`"2222"`:                 {"2222"},
		`/etc/ssh/conf.d/*.conf`: {"/etc/ssh/conf.d/*.conf"},
		`"/a.conf" "/b.conf"`:    {"/a.conf", "/b.conf"},
		`"/a dir/x.conf"`:        {"/a dir/x.conf"},
		`a"b c"d`:                {"ab cd"},
		`"2222`:                  {"2222"},
		`""`:                     {""},
	} {
		if got := sshConfigArgs(input); !reflect.DeepEqual(got, want) {
			t.Fatalf("sshConfigArgs(%q) = %v, want %v", input, got, want)
		}
	}
}
