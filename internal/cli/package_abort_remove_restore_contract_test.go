package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// abortRemoveHarness redirects every absolute install path of a maintainer
// script into a temp prefix and puts a fake systemctl first on PATH, because
// the scripts hardcode /lib/systemd/system, /run/systemd/system and friends.
type abortRemoveHarness struct {
	dir        string
	binDir     string
	systemctl  string
	scriptPath map[string]string
}

func newAbortRemoveHarness(t *testing.T) *abortRemoveHarness {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("maintainer scripts need sh")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skipf("sh not found: %v", err)
	}
	dir := t.TempDir()
	h := &abortRemoveHarness{
		dir:        dir,
		binDir:     filepath.Join(dir, "bin"),
		systemctl:  filepath.Join(dir, "systemctl.log"),
		scriptPath: map[string]string{},
	}
	for _, sub := range []string{"bin", "run-systemd", "run-veil", "lib-systemd", "etc-systemd", "etc-sysctl"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Host commands the scripts call: no-ops, plus an id that reports the
	// service accounts as present so postinstall never tries to create them.
	for _, name := range []string{"getent", "groupadd", "useradd", "usermod", "addgroup", "adduser", "sysctl"} {
		h.writeStub(t, name, "#!/bin/sh\nexit 0\n")
	}
	h.writeStub(t, "id", `#!/bin/sh
case "$1" in
  -u) exit 0 ;;
  -nG) echo "veil veil-proxy veil-mita" ;;
esac
exit 0
`)
	h.writeStub(t, "veil", "#!/bin/sh\nexit 0\n")
	// The fake systemctl records every invocation and answers the instance
	// scans with one live instance per template, so both scripts exercise the
	// per-instance loops.
	h.writeStub(t, "systemctl", `#!/bin/sh
printf '%s\n' "$*" >> "$SYSTEMCTL_LOG"
case "$1" in
  list-units|list-unit-files)
    case "$*" in *veil-hysteria2@*) echo "veil-hysteria2@node1.service loaded active running" ;; esac
    case "$*" in *veil-olcrtc@*) echo "veil-olcrtc@node1.service loaded active running" ;; esac
    case "$*" in *veil-caddy@*) echo "veil-caddy@legacy.service loaded active running" ;; esac
    exit 0
    ;;
  start|enable)
    if [ -n "$SYSTEMCTL_FAIL_SWITCH" ]; then exit 1; fi
    ;;
esac
exit 0
`)
	// Rewrite absolute install paths into the prefix; the controller flow of
	// the scripts is untouched.
	rewriter := strings.NewReplacer(
		"/usr/local/bin/veil", filepath.Join(dir, "bin", "veil"),
		"/run/systemd/system", filepath.Join(dir, "run-systemd"),
		"/run/veil", filepath.Join(dir, "run-veil"),
		"/usr/lib/systemd/system", filepath.Join(dir, "lib-systemd"),
		"/lib/systemd/system", filepath.Join(dir, "lib-systemd"),
		"/etc/systemd/system", filepath.Join(dir, "etc-systemd"),
		"/etc/sysctl.d/99-veil-quic.conf", filepath.Join(dir, "etc-sysctl", "99-veil-quic.conf"),
		// #1424: the package-managed marker must never touch the real /etc in a
		// test, even on an install-mode run — rewrite both the marker and the
		// directory its `[ -d /etc/veil ]` guard checks.
		"/etc/veil/.package-managed", filepath.Join(dir, "etc-veil", ".package-managed"),
		"/etc/veil", filepath.Join(dir, "etc-veil"),
	)
	if err := os.MkdirAll(filepath.Join(dir, "etc-veil"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"preremove", "postinstall"} {
		body, err := os.ReadFile(filepath.Join("..", "..", "packaging", "scripts", name+".sh"))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, name+".sh")
		if err := os.WriteFile(path, []byte(rewriter.Replace(strings.ReplaceAll(string(body), "\r\n", "\n"))), 0o600); err != nil {
			t.Fatal(err)
		}
		h.scriptPath[name] = path
	}
	return h
}

func (h *abortRemoveHarness) writeStub(t *testing.T, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(h.binDir, name), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// run executes a rewritten maintainer script with the fake systemctl and
// returns the systemctl argv lines it recorded.
func (h *abortRemoveHarness) run(t *testing.T, script string, args ...string) (calls []string, output string) {
	t.Helper()
	if err := os.WriteFile(h.systemctl, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", append([]string{h.scriptPath[script]}, args...)...)
	cmd.Dir = h.dir
	cmd.Env = append(os.Environ(),
		"PATH="+h.binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SYSTEMCTL_LOG="+h.systemctl,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %v\n%s", script, args, err, out)
	}
	body, err := os.ReadFile(h.systemctl)
	if err != nil {
		t.Fatalf("reading the systemctl log: %v", err)
	}
	return strings.Split(strings.TrimSpace(string(body)), "\n"), string(out)
}

// verbs returns the unit names a log recorded for one systemctl verb.
func verbs(calls []string, verb string) []string {
	var units []string
	for _, call := range calls {
		if rest, ok := strings.CutPrefix(call, verb+" "); ok {
			units = append(units, rest)
		}
	}
	return units
}

// TestAbortRemoveRestoresTheEnableSetPreremoveDisables locks #1466 and #1465
// together: both are defects of the abort-remove path. preremove disables every
// managed unit on remove, so an aborted removal must re-enable exactly that set
// (minus the two units that have no [Install] section) or the panel, the
// proxies and the backup timer stay disabled after a reboot; and it must not
// start the veil-backup.service oneshot, which would run a real backup and
// prune during the rollback.
func TestAbortRemoveRestoresTheEnableSetPreremoveDisables(t *testing.T) {
	h := newAbortRemoveHarness(t)

	removeCalls, _ := h.run(t, "preremove", "remove")
	disabled := verbs(removeCalls, "disable")
	if len(disabled) < 9 {
		t.Fatalf("preremove remove disabled %d units (%v), expected the full managed set", len(disabled), disabled)
	}
	for _, want := range []string{
		"veil.service", "veil-caddy.service", "veil-mieru.service", "veil-warp.service",
		"veil-backup.timer", "veil-backup.service", "veil-helper.service", "veil-helper.socket",
		"veil-hysteria2@node1.service", "veil-olcrtc@node1.service", "veil-caddy@legacy.service",
	} {
		if !slices.Contains(disabled, want) {
			t.Fatalf("preremove remove did not disable %q: %v", want, disabled)
		}
	}

	abortCalls, _ := h.run(t, "postinstall", "abort-remove")
	enabled := verbs(abortCalls, "enable")
	started := verbs(abortCalls, "start")

	// Every unit preremove disabled has to be re-enabled, except the two with
	// no [Install] section: veil-helper.service is socket-activated and
	// veil-backup.service is timer-triggered, so systemctl enable refuses them.
	wantEnabled := make([]string, 0, len(disabled))
	for _, unit := range disabled {
		if unit == "veil-helper.service" || unit == "veil-backup.service" {
			continue
		}
		wantEnabled = append(wantEnabled, unit)
	}
	slices.Sort(wantEnabled)
	// preremove's instance scan runs list-units and list-unit-files, so an
	// instance can be named twice; the sets are compared, not the call counts.
	wantEnabled = slices.Compact(wantEnabled)
	gotEnabled := slices.Clone(enabled)
	slices.Sort(gotEnabled)
	gotEnabled = slices.Compact(gotEnabled)
	if !slices.Equal(wantEnabled, gotEnabled) {
		t.Fatalf("abort-remove enable set drifted from preremove's disable set\nwant: %v\ngot:  %v", wantEnabled, gotEnabled)
	}

	// The services have to be started again, but never the backup oneshot.
	for _, want := range []string{
		"veil.service", "veil-helper.socket", "veil-caddy.service", "veil-mieru.service",
		"veil-warp.service", "veil-backup.timer",
		"veil-hysteria2@node1.service", "veil-olcrtc@node1.service", "veil-caddy@legacy.service",
	} {
		if !slices.Contains(started, want) {
			t.Fatalf("abort-remove did not start %q: %v", want, started)
		}
	}
	if slices.Contains(started, "veil-backup.service") {
		t.Fatal("abort-remove must not start the veil-backup.service oneshot: that runs a real backup and prune during the rollback (#1465)")
	}
}

// TestAbortRemoveLogsAndContinuesWhenAUnitCannotStart locks the second half of
// #1465: an already-aborted removal must not become a dpkg
// post-installation-script error because a unit is not configured on this host.
func TestAbortRemoveLogsAndContinuesWhenAUnitCannotStart(t *testing.T) {
	h := newAbortRemoveHarness(t)
	cmd := exec.Command("sh", h.scriptPath["postinstall"], "abort-remove")
	cmd.Dir = h.dir
	cmd.Env = append(os.Environ(),
		"PATH="+h.binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SYSTEMCTL_LOG="+h.systemctl,
		"SYSTEMCTL_FAIL_SWITCH=1",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("abort-remove failed the package when a unit could not start: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "warning: could not start") {
		t.Fatalf("abort-remove did not report the failed start:\n%s", out)
	}
	if !strings.Contains(string(out), "warning: could not enable") {
		t.Fatalf("abort-remove did not report the failed enable:\n%s", out)
	}
}
