package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestPostinstallQuicExplicitMaxReadsTunedFiles runs the shipped helper itself:
// the packaged path must hand the operator's sysctl.d value to the drop-in.
// A text-only assertion would not catch the awk/file-list bugs that made the
// first version dead code (#1492 review).
func TestPostinstallQuicExplicitMaxReadsTunedFiles(t *testing.T) {
	body, err := os.ReadFile("../../packaging/scripts/postinstall.sh")
	if err != nil {
		t.Fatal(err)
	}
	code := strings.ReplaceAll(string(body), "\r\n", "\n")
	start := strings.Index(code, "quic_explicit_max() {")
	if start < 0 {
		t.Fatal("postinstall has no quic_explicit_max helper")
	}
	end := strings.Index(code[start:], "\n        # end: quic_explicit_max")
	if end < 0 {
		t.Fatal("could not find the end of quic_explicit_max")
	}
	helper := code[start : start+end]

	dir := t.TempDir()
	tuned := filepath.Join(dir, "90-network-tuning.conf")
	if err := os.WriteFile(tuned, []byte("net.core.rmem_max = 67108864\nnet.core.wmem_max = 33554432\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	clamped := filepath.Join(dir, "99-veil-quic.conf")
	if err := os.WriteFile(clamped, []byte("net.core.rmem_max = 16777216\nnet.core.wmem_max = 16777216\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	missingGlob := filepath.Join(dir, "no-such-dir", "*.conf")

	// Edge cases the first helper missed: an indented ignore-failure line with no
	// trailing newline, a value wider than the shell can compare, and systemd
	// name shadowing — a masked (/dev/null) or overriding file in a higher
	// priority directory hides the copies below it.
	indented := filepath.Join(dir, "95-indented.conf")
	if err := os.WriteFile(indented, []byte("  -net.core.rmem_max=134217728"), 0o644); err != nil {
		t.Fatal(err)
	}
	tooWide := filepath.Join(dir, "97-wide.conf")
	if err := os.WriteFile(tooWide, []byte("net.core.wmem_max=123456789012345678901234\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	highDir, lowDir := t.TempDir(), t.TempDir()
	if err := os.Symlink("/dev/null", filepath.Join(highDir, "90-tuning.conf")); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	if err := os.WriteFile(filepath.Join(lowDir, "90-tuning.conf"), []byte("net.core.rmem_max = 67108864\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	overrideDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(overrideDir, "90-tuning.conf"), []byte("net.core.rmem_max = 33554432\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		key  string
		want string
		args string
	}{
		{"tuned file", "net.core.rmem_max", "67108864", tuned + " " + clamped + " " + missingGlob},
		{"wmem", "net.core.wmem_max", "33554432", tuned + " " + clamped + " " + missingGlob},
		{"indented ignore-failure line without newline", "net.core.rmem_max", "134217728", indented},
		{"too-wide value falls back to the floor", "net.core.wmem_max", "0", tooWide},
		{"masked higher copy hides the lower one", "net.core.rmem_max", "0", filepath.Join(highDir, "*.conf") + " " + filepath.Join(lowDir, "*.conf")},
		{"higher copy wins over the lower one", "net.core.rmem_max", "33554432", filepath.Join(overrideDir, "*.conf") + " " + filepath.Join(lowDir, "*.conf")},
	} {
		script := "#!/bin/sh\nset -eu\n" + helper + "\nquic_explicit_max " + tc.key + " " + tc.args + "\n"
		out, err := exec.Command("sh", "-c", script).CombinedOutput()
		if err != nil {
			t.Fatalf("%s: helper failed: %v\n%s", tc.name, err, out)
		}
		if got := strings.TrimSpace(string(out)); got != tc.want {
			t.Fatalf("%s = %q, want %q", tc.name, got, tc.want)
		}
		if strings.Contains(string(out), "Illegal number") {
			t.Fatalf("%s: helper leaked a shell arithmetic error:\n%s", tc.name, out)
		}
	}
}
