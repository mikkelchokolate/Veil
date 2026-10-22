package cli

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestInstallAcceptanceRejectsMissingAndPrematureRuntimes(t *testing.T) {
	checkBash(t)
	body, err := os.ReadFile("../../scripts/ci/install-acceptance.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(string(body), "\r\n", "\n")
	start := strings.Index(script, "assert_protocol_binaries() {")
	if start < 0 {
		t.Fatal("runtime assertion helper missing")
	}
	end := strings.Index(script[start:], "\n}\n")
	if end < 0 {
		t.Fatal("runtime assertion helper is incomplete")
	}
	helper := strings.ReplaceAll(script[start:start+end+3], "/usr/local/bin/", "${RUNTIME_FIXTURE}/")
	for _, tc := range []struct {
		name, prepare, required string
		wantFailure             bool
	}{
		{name: "panel only"},
		{name: "selected runtime", prepare: `touch "${RUNTIME_FIXTURE}/hysteria"; chmod 755 "${RUNTIME_FIXTURE}/hysteria"`, required: "hysteria"},
		{name: "missing selected runtime", required: "hysteria", wantFailure: true},
		{name: "premature runtime", prepare: `touch "${RUNTIME_FIXTURE}/sing-box"`, wantFailure: true},
		{name: "dangling runtime symlink", prepare: `ln -s "${RUNTIME_FIXTURE}/missing" "${RUNTIME_FIXTURE}/caddy"`, wantFailure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			command := `set -eu
RUNTIME_FIXTURE="$(mktemp -d)"
trap 'rm -rf "${RUNTIME_FIXTURE}"' EXIT
SUDO=""
VEIL_RUNTIME_BINS="hysteria mita caddy olcrtc sing-box"
ci_die() { echo "$*" >&2; exit 1; }
` + helper + "\n" + tc.prepare + "\nassert_protocol_binaries " + tc.required
			out, err := exec.Command("bash", "-c", command).CombinedOutput()
			if (err != nil) != tc.wantFailure {
				t.Fatalf("runtime assertion error = %v, wantFailure = %v: %s", err, tc.wantFailure, out)
			}
		})
	}
}
