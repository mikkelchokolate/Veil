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
	const gone = "https://example.invalid/v1/a.tar.xz"

	for _, tc := range []struct {
		name, code, sha, url, gone string
		wantRC                     int
		wantMarker                 bool
	}{
		{"available and verified", "200", good, gone, gone, 0, false},
		{"available but checksum mismatch", "200", strings.Repeat("0", 64), gone, gone, 1, false},
		{"known-gone URL 404 soft-skips", "404", good, gone, gone, 2, true},
		{"404 for a different (bumped) asset stays fatal", "404", good, "https://example.invalid/v1/b.tar.xz", gone, 1, false},
		{"404 for a bumped tag with a stale asset name stays fatal", "404", good, "https://example.invalid/v2/a.tar.xz", gone, 1, false},
		{"404 for a mirror with the same file name stays fatal", "404", good, "https://example.invalid/ci-deps/a.tar.xz", gone, 1, false},
		{"404 with no known-gone URL stays fatal", "404", good, gone, "", 1, false},
		{"server error stays fatal", "500", good, gone, gone, 1, false},
		{"rate limit stays fatal", "403", good, gone, gone, 1, false},
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
			cmd := exec.Command("bash", "-c", "set -euo pipefail\n"+helper+"\nrc=0; fetch_naive_client \"$URL\" \"$DEST/naive.tar.xz\" \"$PIN\" \"$DEST\" || rc=$?; exit $rc")
			cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "FIXTURE_PAYLOAD="+payload, "FIXTURE_CODE="+tc.code, "PIN="+tc.sha, "DEST="+dir, "URL="+tc.url, "CI_NAIVE_CLIENT_KNOWN_GONE_URL="+tc.gone)
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

// The #1373 escape hatch must name exactly the URL runtimes.sh fetches. Once the
// pin is bumped or repointed (e.g. at the ci-deps mirror), a leftover value
// would be dead at best, so it must be removed in the same change.
func TestCINaiveClientKnownGoneURLIsCurrent(t *testing.T) {
	checkBash(t)
	cmd := exec.Command("bash", "-c", `set -euo pipefail
source ../../scripts/ci/runtimes.sh
printf '%s\n%s\n%s\n' "$(naive_client_url)" "${CI_NAIVE_CLIENT_KNOWN_GONE_URL:-}" "${CI_NAIVE_CLIENT_ASSET}"`)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("sourcing runtimes.sh: %v\n%s", err, stderr.String())
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("unexpected output %q", out)
	}
	if asset := lines[2]; asset == "" || !strings.HasSuffix(lines[0], "/"+asset) || strings.Contains(lines[0], "//"+asset) {
		t.Fatalf("naive_client_url = %q must end in /<tag>/%s with both pins non-empty", lines[0], asset)
	}
	fetched, gone := lines[0], lines[1]
	if !strings.HasPrefix(fetched, "https://") {
		t.Fatalf("naive_client_url = %q, want an https URL", fetched)
	}
	if gone != "" && gone != fetched {
		t.Fatalf("CI_NAIVE_CLIENT_KNOWN_GONE_URL = %q no longer matches the fetched naive URL %q; remove it (#1373)", gone, fetched)
	}
	body, err := os.ReadFile("../../scripts/ci/runtimes.sh")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `fetch_naive_client "$(naive_client_url)"`) {
		t.Fatal("runtimes.sh must fetch the naive client via naive_client_url so the known-gone check sees the real URL")
	}
}

func TestCIE2ENaiveGateRunsStrictPathUnlessKnownGone(t *testing.T) {
	checkBash(t)
	body, err := os.ReadFile("../../scripts/ci/e2e.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(string(body), "\r\n", "\n")
	gateStart := strings.Index(script, "naive_client_gate() {")
	gateEnd := strings.Index(script, "naive_client_gate \"${runtime_dir}\"")
	runStart := strings.Index(script, "if [ \"${naive_client_available}\" -eq 1 ]; then\n  ci_run e2e-naiveproxy")
	if gateStart < 0 || gateEnd < gateStart || runStart < 0 {
		t.Fatal("naive gate or gated naive data-path block is missing from e2e.sh")
	}
	runEnd := runStart + strings.Index(script[runStart:], "\nfi\n") + len("\nfi\n")
	harness := "set -euo pipefail\n" + script[gateStart:gateEnd] +
		"ci_run() { echo \"RAN $1\"; }\nci_assert_test_passed() { echo \"ASSERTED $2\"; }\nCI_ARTIFACT_DIR=/x\n" +
		"naive_client_gate \"$RUNTIME\"\n" + script[runStart:runEnd]
	for _, tc := range []struct {
		name           string
		marker, binary bool
		wantRun        bool
	}{
		{"binary present runs strict path", false, true, true},
		{"marker but binary present still runs", true, true, true},
		{"marker and no binary skips", true, false, false},
		{"no marker and no binary still attempts (fails closed)", false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.marker {
				if err := os.WriteFile(filepath.Join(dir, "naive.unavailable"), []byte("gone"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if tc.binary {
				if err := os.WriteFile(filepath.Join(dir, "naive"), []byte("#!/bin/true\n"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command("bash", "-c", harness)
			cmd.Env = append(os.Environ(), "RUNTIME="+dir)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("harness failed: %v: %s", err, out)
			}
			ran := strings.Contains(string(out), "RAN e2e-naiveproxy") && strings.Contains(string(out), "ASSERTED TestRequiredNaiveProxyDataPath")
			if ran != tc.wantRun {
				t.Fatalf("naive data path ran = %v, want %v; output: %s", ran, tc.wantRun, out)
			}
			if !tc.wantRun && !strings.Contains(string(out), "::warning") {
				t.Fatalf("skip must emit ::warning::; output: %s", out)
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
