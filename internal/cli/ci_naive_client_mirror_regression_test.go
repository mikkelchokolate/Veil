package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const naiveMirrorURL = "https://github.com/mikkelchokolate/Veil/releases/download/ci-deps/naiveproxy-v150.0.7871.63-1-linux-x64.tar.xz"

// Issue #1373: the naiveproxy client comes from our ci-deps mirror first, with
// upstream as a fallback; both are verified against the same pin, and the
// e2e naive data path is strict again (no soft-skip).
func TestCINaiveClientMirrorPin(t *testing.T) {
	body, err := os.ReadFile("../../scripts/ci/versions.sh")
	if err != nil {
		t.Fatal(err)
	}
	versions := string(body)
	if !strings.Contains(versions, `CI_NAIVE_CLIENT_MIRROR_URL="`+naiveMirrorURL+`"`) {
		t.Fatal("versions.sh must pin the ci-deps mirror URL for the naive client")
	}
	if !regexp.MustCompile(`(?m)^CI_NAIVE_CLIENT_SHA256="[0-9a-f]{64}"$`).MatchString(versions) {
		t.Fatal("versions.sh must pin a 64-hex naive client sha256")
	}
	if strings.Contains(versions, "0c4f506ce66a7881892fd6932b542c53fc06ac2351987756096c61e753c687bf") {
		t.Fatal("the deleted upstream build checksum must no longer be pinned")
	}
	for _, f := range []string{"versions.sh", "runtimes.sh", "e2e.sh"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "scripts", "ci", f))
		if err != nil {
			t.Fatal(err)
		}
		for _, gone := range []string{"KNOWN_GONE", "naive.unavailable", "naive_client_gate", "fetch_naive_client", "naive e2e SKIPPED"} {
			if strings.Contains(string(data), gone) {
				t.Fatalf("%s still contains the #1373 soft-skip (%q); the naive e2e path must be strict", f, gone)
			}
		}
	}
	e2e, err := os.ReadFile("../../scripts/ci/e2e.sh")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`(?m)^ci_run e2e-naiveproxy run_proto TestRequiredNaiveProxyDataPath\n` +
		`ci_assert_test_passed "\$\{CI_ARTIFACT_DIR\}/e2e-naiveproxy.log" TestRequiredNaiveProxyDataPath$`).Match(e2e) {
		t.Fatal("e2e.sh must unconditionally run and assert TestRequiredNaiveProxyDataPath")
	}
}

func TestCINaiveClientFetchMirrorThenUpstreamBothVerified(t *testing.T) {
	checkBash(t)
	body, err := os.ReadFile("../../scripts/ci/runtimes.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(string(body), "\r\n", "\n")
	hStart := strings.Index(script, "fetch_verified() {")
	hEnd := strings.Index(script[hStart:], "\n}\n")
	site := regexp.MustCompile(`(?s)    fetch_verified "\$\{CI_NAIVE_CLIENT_MIRROR_URL\}" naive\.tar\.xz "\$\{CI_NAIVE_CLIENT_SHA256\}" \\\n      \|\| fetch_verified "https://github\.com/klzgrad/naiveproxy/releases/download/\$\{CI_NAIVE_CLIENT_TAG\}/\$\{CI_NAIVE_CLIENT_ASSET\}" naive\.tar\.xz "\$\{CI_NAIVE_CLIENT_SHA256\}"\n`).FindString(script)
	if hStart < 0 || hEnd < 0 || site == "" {
		t.Fatal("naive client must be fetched via fetch_verified from the mirror, then upstream, with the same pin")
	}
	helper := script[hStart : hStart+hEnd+3]
	good := "naive-tarball"
	sum := sha256.Sum256([]byte(good))
	pin := hex.EncodeToString(sum[:])

	for _, tc := range []struct {
		name, mirror, upstream string // "" = HTTP failure
		wantOK                 bool
		wantHits               string
	}{
		{"mirror verified, upstream untouched", good, good, true, "M"},
		{"mirror down, upstream verified", "", good, true, "MU"},
		{"mirror tampered, upstream verified", "evil", good, true, "MU"},
		{"both down fails", "", "", false, "MU"},
		{"mirror writes a correct body then errors, upstream verified", "partial-ok", good, true, "MU"},
		{"mirror tampered, upstream tampered fails", "evil", "evil", false, "MU"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			harness := `set -euo pipefail
curl() {
  local url="${@: -3:1}" out="${@: -1}"
  case "${url}" in
    *ci-deps*) printf M >>"$HITS"; [ -n "$MIRROR" ] || return 22
      if [ "$MIRROR" = partial-ok ]; then printf '%s' "$GOOD" >"$out"; return 18; fi
      printf '%s' "$MIRROR" >"$out" ;;
    *klzgrad*) printf U >>"$HITS"; [ -n "$UPSTREAM" ] || return 22; printf '%s' "$UPSTREAM" >"$out" ;;
    *) return 99 ;;
  esac
}
` + helper + `
CI_NAIVE_CLIENT_MIRROR_URL=` + naiveMirrorURL + ` CI_NAIVE_CLIENT_TAG=v150.0.7871.63-1 CI_NAIVE_CLIENT_ASSET=naiveproxy-v150.0.7871.63-1-linux-x64.tar.xz
` + site + `cmp -s naive.tar.xz <(printf '%s' "$GOOD")
`
			cmd := exec.Command("bash", "-c", harness)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "HITS="+filepath.Join(dir, "hits"), "MIRROR="+tc.mirror, "UPSTREAM="+tc.upstream,
				"GOOD="+good, "CI_NAIVE_CLIENT_SHA256="+pin, "CI_RUNTIME_DOWNLOAD_CACHE=")
			out, err := cmd.CombinedOutput()
			if (err == nil) != tc.wantOK {
				t.Fatalf("ok = %v, want %v; output: %s", err == nil, tc.wantOK, out)
			}
			hits, _ := os.ReadFile(filepath.Join(dir, "hits"))
			if string(hits) != tc.wantHits {
				t.Fatalf("download order = %q, want %q", hits, tc.wantHits)
			}
		})
	}
}
