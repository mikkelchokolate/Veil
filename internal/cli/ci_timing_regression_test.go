package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
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
