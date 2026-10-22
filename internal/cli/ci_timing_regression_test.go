package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestCITimingMergePreservesProcessWallTimesAndFastRoots(t *testing.T) {
	dir := t.TempDir()
	writeJSON := func(name, body string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	first := writeJSON("first.json", `[{"task":"package-001","package":"example/pkg","elapsed_ms":15000}]`)
	second := writeJSON("second.json", `[{"task":"package-002","package":"example/other","elapsed_ms":8000}]`)
	command := exec.Command("python3", "../../scripts/ci/test-inventory.py", "--artifact-dir", dir,
		"--merge-task-timings", first, "--merge-task-timings", second)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("merge task timings: %v: %s", err, out)
	}
	roots := writeJSON("roots.json", `[{"package":"example/pkg","root":"TestFast"},{"package":"example/other","root":"TestSlow"}]`)
	log := writeJSON("tests.json", `{"Action":"pass","Package":"example/pkg","Test":"TestFast","Elapsed":0}
{"Action":"pass","Package":"example/other","Test":"TestSlow","Elapsed":6}
`)
	command = exec.Command("python3", "../../scripts/ci/test-inventory.py", "--artifact-dir", dir,
		"--roots-json", roots, "--log", log)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("report merged timings: %v: %s", err, out)
	}
	body, err := os.ReadFile(filepath.Join(dir, "timing-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Roots []struct {
			Root      string `json:"root"`
			ElapsedMs int    `json:"elapsedMs"`
		} `json:"roots"`
		Packages []struct {
			Package       string `json:"package"`
			ProcessWallMs int    `json:"processWallMs"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(body, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Roots) != 2 || manifest.Roots[0].Root != "TestFast" || manifest.Roots[0].ElapsedMs != 0 {
		t.Fatalf("zero-duration measured root was lost: %s", body)
	}
	walls := map[string]int{}
	for _, row := range manifest.Packages {
		walls[row.Package] = row.ProcessWallMs
	}
	if walls["example/pkg"] != 15000 || walls["example/other"] != 8000 {
		t.Fatalf("merged process wall times were lost: %s", body)
	}
	for _, tc := range []struct{ name, body string }{
		{"duplicate", `[{"task":"package-001","package":"example/pkg","elapsed_ms":1}]`},
		{"negative", `[{"task":"package-003","package":"example/pkg","elapsed_ms":-1}]`},
		{"malformed", `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := writeJSON(tc.name+".json", tc.body)
			cmd := exec.Command("python3", "../../scripts/ci/test-inventory.py", "--artifact-dir", dir,
				"--merge-task-timings", first, "--merge-task-timings", input)
			if out, err := cmd.CombinedOutput(); err == nil {
				t.Fatalf("invalid timing merge unexpectedly passed: %s", out)
			}
		})
	}
}

func TestCICachedRuntimesRequireExactPins(t *testing.T) {
	checkBash(t)
	body, err := os.ReadFile("../../scripts/ci/runtimes.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(string(body), "\r\n", "\n")
	start := strings.Index(script, "caddy_test_runtime_is_pinned() {")
	end := strings.Index(script, "install_pinned_caddy_binary() {")
	if start < 0 || end < start {
		t.Fatal("cached runtime pin checks are missing")
	}
	helpers := script[start:end]
	for _, tc := range []struct {
		name, caddy, proxy, singbox, modules string
		wantFailure                          bool
	}{
		{"matching pins", "v2.11.4", "v0.0.0-20260101120000-d62c80d3dd2c", "1.14.0", "http.handlers.forward_proxy", false},
		{"wrong Caddy", "v2.11.3", "v0.0.0-20260101120000-d62c80d3dd2c", "1.14.0", "http.handlers.forward_proxy", true},
		{"wrong proxy", "v2.11.4", "v0.0.0-20260101120000-d62c80d3dd2d", "1.14.0", "http.handlers.forward_proxy", true},
		{"missing proxy module", "v2.11.4", "v0.0.0-20260101120000-d62c80d3dd2c", "1.14.0", "http.handlers.other", true},
		{"wrong singbox", "v2.11.4", "v0.0.0-20260101120000-d62c80d3dd2c", "1.13.0", "http.handlers.forward_proxy", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			binary := filepath.Join(dir, "runtime")
			if err := os.WriteFile(binary, []byte("#!/usr/bin/env bash\ncase \"$1\" in\nlist-modules) echo \"$FIXTURE_MODULES\";;\nversion) echo \"sing-box version $FIXTURE_SINGBOX\";;\nesac\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("bash", "-c", `set -euo pipefail
CI_CADDY_VERSION=v2.11.4
CI_FORWARDPROXY_VERSION=d62c80d3dd2c706b6b87579844d2397bddd18317
CI_SINGBOX_TAG=v1.14.0
go() { printf 'dep github.com/caddyserver/caddy/v2 %s\n=> github.com/klzgrad/forwardproxy %s\n' "$FIXTURE_CADDY" "$FIXTURE_PROXY"; }
`+helpers+`caddy_test_runtime_is_pinned "$FIXTURE_BINARY"
singbox_test_runtime_is_pinned "$FIXTURE_BINARY"
`)
			cmd.Env = append(os.Environ(), "FIXTURE_BINARY="+binary, "FIXTURE_CADDY="+tc.caddy,
				"FIXTURE_PROXY="+tc.proxy, "FIXTURE_SINGBOX="+tc.singbox, "FIXTURE_MODULES="+tc.modules)
			if out, err := cmd.CombinedOutput(); (err != nil) != tc.wantFailure {
				t.Fatalf("cached runtime validation error=%v, wantFailure=%v: %s", err, tc.wantFailure, out)
			}
		})
	}
}

func TestCIRuntimeDownloadCacheRequiresPinnedSHA(t *testing.T) {
	checkBash(t)
	body, err := os.ReadFile("../../scripts/ci/runtimes.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(string(body), "\r\n", "\n")
	start := strings.Index(script, "fetch_verified() {")
	end := strings.Index(script, "caddy_test_runtime_is_pinned() {")
	if start < 0 || end < start {
		t.Fatal("verified download helpers are missing")
	}
	helpers := script[start:end]
	const good = "pinned asset bytes\n"
	sum := sha256.Sum256([]byte(good))
	pin := hex.EncodeToString(sum[:])
	run := func(t *testing.T, cacheDir, download, cached string) (string, error) {
		t.Helper()
		if cached != "" {
			if err := os.MkdirAll(cacheDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(cacheDir, pin), []byte(cached), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		work := t.TempDir()
		cmd := exec.Command("bash", "-c", `set -euo pipefail
curl() { # stub: <flags...> <url> -o <file>; empty fixture = upstream 404
  [ -n "${FIXTURE_DOWNLOAD}" ] || return 22
  printf '%s' "${FIXTURE_DOWNLOAD}" > "${@: -1}"
}
`+helpers+`fetch_verified https://example.invalid/asset asset "$FIXTURE_PIN"
cat asset
`)
		cmd.Dir = work
		cmd.Env = append(os.Environ(), "CI_RUNTIME_DOWNLOAD_CACHE="+cacheDir,
			"FIXTURE_DOWNLOAD="+download, "FIXTURE_PIN="+pin)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	cacheEntry := func(t *testing.T, cacheDir string) (string, bool) {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(cacheDir, pin))
		if os.IsNotExist(err) {
			return "", false
		}
		if err != nil {
			t.Fatal(err)
		}
		return string(data), true
	}

	t.Run("cold cache stores verified download", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "cache")
		if out, err := run(t, dir, good, ""); err != nil || !strings.HasSuffix(out, good) {
			t.Fatalf("verified download failed: %v: %s", err, out)
		}
		if got, ok := cacheEntry(t, dir); !ok || got != good {
			t.Fatalf("verified download was not cached: %q", got)
		}
	})
	t.Run("warm cache survives upstream outage", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "cache")
		if out, err := run(t, dir, "", good); err != nil || !strings.HasSuffix(out, good) {
			t.Fatalf("verified cache entry was not used: %v: %s", err, out)
		}
	})
	t.Run("tampered cache entry is rejected and dropped", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "cache")
		if out, err := run(t, dir, "", "tampered\n"); err == nil {
			t.Fatalf("tampered cache entry was accepted: %s", out)
		}
		if _, ok := cacheEntry(t, dir); ok {
			t.Fatal("tampered cache entry was kept")
		}
	})
	t.Run("tampered cache entry is repaired from upstream", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "cache")
		if out, err := run(t, dir, good, "tampered\n"); err != nil || !strings.HasSuffix(out, good) {
			t.Fatalf("upstream fallback failed: %v: %s", err, out)
		}
		if got, _ := cacheEntry(t, dir); got != good {
			t.Fatalf("cache entry was not repaired: %q", got)
		}
	})
	t.Run("mismatched download is rejected and never cached", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "cache")
		if out, err := run(t, dir, "substituted\n", ""); err == nil {
			t.Fatalf("mismatched download was accepted: %s", out)
		}
		if _, ok := cacheEntry(t, dir); ok {
			t.Fatal("mismatched download was cached")
		}
	})
	t.Run("completeness requires every pinned asset", func(t *testing.T) {
		dir := t.TempDir()
		pins := map[string]string{}
		for _, name := range []string{"HYSTERIA", "MITA", "MIERU_CLIENT", "NAIVE_CLIENT", "SINGBOX"} {
			content := "asset " + name + "\n"
			s := sha256.Sum256([]byte(content))
			pins[name] = hex.EncodeToString(s[:])
			if err := os.WriteFile(filepath.Join(dir, pins[name]), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		check := func() error {
			env := ""
			for name, sha := range pins {
				env += "CI_" + name + "_SHA256=" + sha + "\n"
			}
			cmd := exec.Command("bash", "-c", "set -euo pipefail\n"+env+helpers+`pinned_runtime_download_cache_complete "$FIXTURE_DIR"`)
			cmd.Env = append(os.Environ(), "FIXTURE_DIR="+dir)
			return cmd.Run()
		}
		if err := check(); err != nil {
			t.Fatalf("complete cache reported incomplete: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, pins["NAIVE_CLIENT"]), []byte("tampered\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := check(); err == nil {
			t.Fatal("cache with a tampered asset reported complete")
		}
		if err := os.Remove(filepath.Join(dir, pins["NAIVE_CLIENT"])); err != nil {
			t.Fatal(err)
		}
		if err := check(); err == nil {
			t.Fatal("cache missing the naive client reported complete")
		}
	})
}

func TestCIRuntimeDownloadShasMatchFetchSites(t *testing.T) {
	body, err := os.ReadFile("../../scripts/ci/runtimes.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := stripHashComments(t, strings.ReplaceAll(string(body), "\r\n", "\n"))
	start := strings.Index(script, "pinned_runtime_download_shas() {")
	if start < 0 {
		t.Fatal("pinned_runtime_download_shas is missing")
	}
	end := strings.Index(script[start:], "\n}")
	if end < 0 {
		t.Fatal("pinned_runtime_download_shas is unterminated")
	}
	pinVar := regexp.MustCompile(`\$\{(CI_[A-Z0-9_]+_SHA256)\}`)
	listed := map[string]bool{}
	for _, m := range pinVar.FindAllStringSubmatch(script[start:start+end], -1) {
		listed[m[1]] = true
	}
	fetchSite := regexp.MustCompile(`\bfetch_verified\s+"[^"]*"\s+\S+\s+"\$\{(CI_[A-Z0-9_]+_SHA256)\}"`)
	fetched := map[string]bool{}
	for _, m := range fetchSite.FindAllStringSubmatch(script, -1) {
		fetched[m[1]] = true
	}
	if len(fetched) == 0 {
		t.Fatal("no fetch_verified call sites found")
	}
	// Any other fetch_verified call (e.g. a non-literal pin) would escape the set comparison.
	if calls := regexp.MustCompile(`\bfetch_verified\s`).FindAllString(script, -1); len(calls) != len(fetchSite.FindAllString(script, -1)) {
		t.Fatalf("every fetch_verified call must pass a literal ${CI_*_SHA256} pin: %d calls, %d recognised", len(calls), len(fetchSite.FindAllString(script, -1)))
	}
	for v := range fetched {
		if !listed[v] {
			t.Errorf("%s is downloaded via fetch_verified but missing from pinned_runtime_download_shas (cache key and completeness would ignore it)", v)
		}
	}
	for v := range listed {
		if !fetched[v] {
			t.Errorf("%s is listed in pinned_runtime_download_shas but never fetched (cache could never be complete)", v)
		}
	}
}
