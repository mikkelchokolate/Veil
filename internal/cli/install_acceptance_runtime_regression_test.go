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

func TestInstallAcceptanceRequiresOperationalSystemdManagerNotIdleBootQueue(t *testing.T) {
	checkBash(t)
	body, err := os.ReadFile("../../scripts/ci/install-acceptance.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(string(body), "\r\n", "\n")
	start := strings.Index(script, "systemd_ready=1")
	end := strings.Index(script, `if [ "$(id -u)" -eq 0 ]`)
	if start < 0 || end <= start {
		t.Fatal("acceptance systemd manager gate missing")
	}
	for _, state := range []string{"starting", "running", "degraded", "initializing", "offline", "maintenance", "stopping", "unknown"} {
		t.Run(state, func(t *testing.T) {
			command := exec.Command("bash", "-c", `set -euo pipefail
systemctl() { printf '%s\n' "$CI_SYSTEMD_FIXTURE_STATE"; }
seq() { printf '1\n'; }
sleep() { :; }
cat() { printf 'systemd\n'; }
ci_die() { echo "$*" >&2; exit 1; }
`+script[start:end])
			command.Env = append(os.Environ(), "CI_SYSTEMD_FIXTURE_STATE="+state)
			out, err := command.CombinedOutput()
			wantFailure := state != "starting" && state != "running" && state != "degraded"
			if (err != nil) != wantFailure {
				t.Fatalf("systemd state %s error=%v wantFailure=%v: %s", state, err, wantFailure, out)
			}
		})
	}
}
