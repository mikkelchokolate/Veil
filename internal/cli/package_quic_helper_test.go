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
	end := strings.Index(code[start:], "\n        }\n")
	if end < 0 {
		t.Fatal("could not find the end of quic_explicit_max")
	}
	helper := code[start : start+end+len("\n        }")]

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

	for _, tc := range []struct {
		key  string
		want string
	}{
		{"net.core.rmem_max", "67108864"},
		{"net.core.wmem_max", "33554432"},
	} {
		script := "#!/bin/sh\nset -eu\n" + helper + "\nquic_explicit_max " + tc.key + " " + tuned + " " + clamped + " " + missingGlob + "\n"
		out, err := exec.Command("sh", "-c", script).CombinedOutput()
		if err != nil {
			t.Fatalf("%s helper failed: %v\n%s", tc.key, err, out)
		}
		if got := strings.TrimSpace(string(out)); got != tc.want {
			t.Fatalf("%s = %q, want %q (the missing glob must not break the scan)", tc.key, got, tc.want)
		}
	}
}
