package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Issue #1373: upstream removed the naiveproxy client release binaries. Only a
// 404 for the pinned asset may soft-skip the naive e2e data path; every other
// failure and any checksum mismatch must stay fatal.
func TestCINaiveClientFetchOnlySkipsOn404(t *testing.T) {
	checkBash(t)
	body, err := os.ReadFile("../../scripts/ci/runtimes.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(string(body), "\r\n", "\n")
	start := strings.Index(script, "NAIVE_CLIENT_UNAVAILABLE_MARKER=")
	end := strings.Index(script, "caddy_test_runtime_is_pinned() {")
	if start < 0 || end < start {
		t.Fatal("fetch_naive_client helper is missing")
	}
	helper := script[start:end]
	payload := "naive-tarball"
	sum := sha256.Sum256([]byte(payload))
	good := hex.EncodeToString(sum[:])

	for _, tc := range []struct {
		name, code, sha string
		wantRC          int
		wantMarker      bool
	}{
		{"available and verified", "200", good, 0, false},
		{"available but checksum mismatch", "200", strings.Repeat("0", 64), 1, false},
		{"pinned asset 404 soft-skips", "404", good, 2, true},
		{"server error stays fatal", "500", good, 1, false},
		{"rate limit stays fatal", "403", good, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			bin := filepath.Join(dir, "bin")
			if err := os.Mkdir(bin, 0o755); err != nil {
				t.Fatal(err)
			}
			fakeCurl := "#!/usr/bin/env bash\nout=\nwhile [ $# -gt 0 ]; do case \"$1\" in --output) out=\"$2\"; shift;; esac; shift; done\nprintf '%s' \"$FIXTURE_PAYLOAD\" >\"$out\"\nprintf '%s' \"$FIXTURE_CODE\"\n"
			if err := os.WriteFile(filepath.Join(bin, "curl"), []byte(fakeCurl), 0o755); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("bash", "-c", "set -euo pipefail\n"+helper+"\nrc=0; fetch_naive_client https://example.invalid/naive.tar.xz \"$DEST/naive.tar.xz\" \"$PIN\" \"$DEST\" || rc=$?; exit $rc")
			cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "FIXTURE_PAYLOAD="+payload, "FIXTURE_CODE="+tc.code, "PIN="+tc.sha, "DEST="+dir)
			out, _ := cmd.CombinedOutput()
			if got := cmd.ProcessState.ExitCode(); got != tc.wantRC {
				t.Fatalf("exit code = %d, want %d; output: %s", got, tc.wantRC, out)
			}
			_, statErr := os.Stat(filepath.Join(dir, "naive.unavailable"))
			if (statErr == nil) != tc.wantMarker {
				t.Fatalf("marker present = %v, want %v", statErr == nil, tc.wantMarker)
			}
			if tc.wantMarker && !strings.Contains(string(out), "::warning") {
				t.Fatalf("404 skip must emit a ::warning:: annotation; output: %s", out)
			}
		})
	}
}

func TestCIE2ENaiveSkipIsLoudAndScoped(t *testing.T) {
	body, err := os.ReadFile("../../scripts/ci/e2e.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(body)
	for _, required := range []string{
		`naive.unavailable`,
		`::warning title=naive e2e SKIPPED::`,
		`GITHUB_STEP_SUMMARY`,
		`ci_assert_test_passed "${CI_ARTIFACT_DIR}/e2e-naiveproxy.log" TestRequiredNaiveProxyDataPath`,
		`ci_assert_test_passed "${CI_ARTIFACT_DIR}/e2e-hysteria2.log" TestRequiredHysteria2DataPath`,
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("e2e.sh is missing %q", required)
		}
	}
}
