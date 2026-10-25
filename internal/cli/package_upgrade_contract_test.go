package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Regression for #354: /etc/veil/panel TLS is shared with the protocol units
// (User=veil-proxy), so the package migration path must group it veil-proxy —
// the same contract as /etc/veil/generated and /etc/veil/tls. Since #1143 the
// normalization runs inside `veil helper migrate` (hostaccess.Migrate), which
// implements the root:veil-proxy grouping on pinned descriptors; the script
// must delegate to it rather than hand-rolling chown/chmod walks.
func TestPostinstallGroupsPanelTLSForProxyReaders(t *testing.T) {
	body, err := os.ReadFile("../../packaging/scripts/postinstall.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := stripHashComments(t, strings.ReplaceAll(string(body), "\r\n", "\n"))
	if !strings.Contains(script, "/usr/local/bin/veil helper migrate") {
		t.Fatalf("postinstall.sh must delegate ownership normalization to `veil helper migrate`:\n%s", script)
	}
	// /etc/veil/panel must stay inside the documented runtime-shared contract
	// (the delegate implements root:veil-proxy grouping for it; the grouping
	// itself is asserted by the hostaccess tests). The script documents the
	// coverage in the delegation comment, which stripHashComments removes —
	// so check the raw body.
	if !strings.Contains(string(body), "/etc/veil/panel") {
		t.Fatalf("postinstall.sh must keep /etc/veil/panel in the runtime-shared contract:\n%s", script)
	}
	// #1143: service-owned trees must never be walked by path-following shell
	// chmod/chown again — a mid-walk symlink swap lands on arbitrary targets.
	for _, banned := range []string{"-exec chmod", "chown -R"} {
		if strings.Contains(script, banned) {
			t.Fatalf("postinstall.sh must not reintroduce path-following %q walks over service-owned trees:\n%s", banned, script)
		}
	}
	if strings.Contains(script, "chown -R root:veil /etc/veil/panel") {
		t.Fatal("postinstall.sh must not regroup /etc/veil/panel back to the panel-only veil group")
	}
}

func TestAPKUpgradeRunsHardenedConfigurationHook(t *testing.T) {
	body, err := os.ReadFile("../../packaging/nfpm.yaml")
	if err != nil {
		t.Fatal(err)
	}
	// Parsed YAML: the postupgrade hook must live under apk.scripts — a
	// `# postupgrade:` comment or a scripts key in an overrides block cannot
	// satisfy this (issue #774).
	var cfg struct {
		APK struct {
			Scripts map[string]string `yaml:"scripts"`
		} `yaml:"apk"`
		Scripts map[string]string `yaml:"scripts"`
	}
	if err := yaml.Unmarshal(body, &cfg); err != nil {
		t.Fatalf("nfpm.yaml is not valid YAML: %v", err)
	}
	if cfg.APK.Scripts["postupgrade"] != "packaging/scripts/postinstall.sh" {
		t.Fatalf("nfpm apk.scripts.postupgrade = %q, want packaging/scripts/postinstall.sh", cfg.APK.Scripts["postupgrade"])
	}
	if cfg.Scripts["postinstall"] != "packaging/scripts/postinstall.sh" {
		t.Fatalf("nfpm scripts.postinstall = %q, want packaging/scripts/postinstall.sh", cfg.Scripts["postinstall"])
	}
}

// TestVendorUnitsAndSysctlAreNotConffiles locks issue #475: packaged systemd
// units and the QUIC sysctl drop-in must be plain package-owned files so
// `dpkg -r`/`rpm -e`/`apk del` remove them. As `type: config` they would
// survive a plain remove until purge.
func TestVendorUnitsAndSysctlAreNotConffiles(t *testing.T) {
	body, err := os.ReadFile("../../packaging/nfpm.yaml")
	if err != nil {
		t.Fatal(err)
	}
	config := strings.ReplaceAll(string(body), "\r\n", "\n")
	// Walk the contents entries: any entry sourced from packaging/systemd or
	// packaging/sysctl must not carry `type: config` before the next entry.
	currentVendorEntry := ""
	for _, line := range strings.Split(config, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- src:") {
			src := strings.TrimSpace(strings.TrimPrefix(trimmed, "- src:"))
			if strings.HasPrefix(src, "packaging/systemd/") || strings.HasPrefix(src, "packaging/sysctl/") {
				currentVendorEntry = src
			} else {
				currentVendorEntry = ""
			}
			continue
		}
		if strings.HasPrefix(trimmed, "- dst:") || strings.HasPrefix(trimmed, "- type:") {
			currentVendorEntry = ""
			continue
		}
		if currentVendorEntry != "" && trimmed == "type: config" {
			t.Fatalf("nfpm entry %s must not be type: config (vendor files are package-owned, not conffiles):\n%s", currentVendorEntry, config)
		}
	}
}

// TestPackageScriptsCoverBackupService locks issue #480/#494: the backup
// *service* is managed asymmetrically from its timer in neither direction —
// preremove stops/disables it and postinstall restarts it on upgrade.
func TestPackageScriptsCoverBackupService(t *testing.T) {
	preremove, err := os.ReadFile("../../packaging/scripts/preremove.sh")
	if err != nil {
		t.Fatal(err)
	}
	pre := stripHashComments(t, strings.ReplaceAll(string(preremove), "\r\n", "\n"))
	if !strings.Contains(pre, "stop_disable_unit veil-backup.service") {
		t.Fatal("preremove.sh must stop/disable veil-backup.service, not only the timer")
	}
	postinstall, err := os.ReadFile("../../packaging/scripts/postinstall.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := stripHashComments(t, strings.ReplaceAll(string(postinstall), "\r\n", "\n"))
	if !strings.Contains(script, "try-restart veil.service veil-helper.service veil-helper.socket veil-caddy.service veil-mieru.service veil-warp.service veil-backup.service veil-backup.timer") {
		t.Fatal("postinstall.sh try-restart list must include veil-backup.service")
	}
	// Operator-authored units must never be deleted just for sharing the
	// well-known ExecStart line (issue #488): only the Generated marker or a
	// byte-identical copy justify removal.
	if strings.Contains(script, "grep -q 'ExecStart=/usr/local/bin/veil serve'") {
		t.Fatal("postinstall.sh must not remove /etc units by ExecStart match alone")
	}
	// daemon-reload/enable/try-restart are fail-closed on a live systemd and
	// skipped where systemd is not the running init (issues #498, #504).
	if !strings.Contains(script, "[ -d /run/systemd/system ]") {
		t.Fatal("postinstall.sh must gate systemctl calls on a running systemd")
	}
	if strings.Contains(script, "daemon-reload >/dev/null 2>&1 || true") ||
		strings.Contains(script, "enable veil-helper.socket >/dev/null 2>&1 || true") {
		t.Fatal("postinstall.sh must not silence daemon-reload/enable failures")
	}
	for _, line := range strings.Split(script, "\n") {
		if strings.Contains(line, "try-restart") && strings.Contains(line, "|| true") {
			t.Fatalf("postinstall.sh must not silence try-restart failures: %q", line)
		}
	}
}

// TestPackageScriptsCoverLegacyCaddyInstances locks issue #375: legacy
// pre-consolidation veil-caddy@<name>.service units are not in the current
// catalog, so package remove must match them explicitly — preremove
// stops/disables them and sweeps their wants links, postremove clears stray
// vendor-dir unit files.
func TestPackageScriptsCoverLegacyCaddyInstances(t *testing.T) {
	preremove, err := os.ReadFile("../../packaging/scripts/preremove.sh")
	if err != nil {
		t.Fatal(err)
	}
	pre := stripHashComments(t, strings.ReplaceAll(string(preremove), "\r\n", "\n"))
	if !strings.Contains(pre, "stop_disable_matching_units 'veil-caddy@*.service'") {
		t.Fatalf("preremove.sh must stop/disable legacy veil-caddy@* instances:\n%s", pre)
	}
	if !strings.Contains(pre, "multi-user.target.wants") ||
		!strings.Contains(pre, `/veil-caddy@*.service`) {
		t.Fatalf("preremove.sh must sweep dangling veil-caddy@* wants links:\n%s", pre)
	}
	postremove, err := os.ReadFile("../../packaging/scripts/postremove.sh")
	if err != nil {
		t.Fatal(err)
	}
	post := stripHashComments(t, strings.ReplaceAll(string(postremove), "\r\n", "\n"))
	if !strings.Contains(post, `"$dir"/veil-caddy@*.service`) {
		t.Fatalf("postremove.sh must sweep legacy veil-caddy@* vendor unit files:\n%s", post)
	}
	uninstallSh, err := os.ReadFile("../../scripts/uninstall.sh")
	if err != nil {
		t.Fatal(err)
	}
	sh := stripHashComments(t, strings.ReplaceAll(string(uninstallSh), "\r\n", "\n"))
	if !strings.Contains(sh, "'veil-caddy@*'") {
		t.Fatalf("uninstall.sh leftover path must stop legacy veil-caddy@* instances:\n%s", sh)
	}
}

// TestPostremoveCleansPackagedLeftovers locks issue #485: postremove cannot
// assume the package manager removed unit/sysctl files (conffile leftovers
// from pre-#475 packages survive plain remove), so it sweeps vendor paths on
// remove/purge while skipping upgrades.
func TestPostremoveCleansPackagedLeftovers(t *testing.T) {
	body, err := os.ReadFile("../../packaging/scripts/postremove.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := stripHashComments(t, strings.ReplaceAll(string(body), "\r\n", "\n"))
	for _, want := range []string{
		"/lib/systemd/system",
		"/usr/lib/systemd/system",
		"/etc/sysctl.d/99-veil-quic.conf",
		"daemon-reload",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("postremove.sh missing %q:\n%s", want, script)
		}
	}
	// #777: the upgrade gate must actually short-circuit BEFORE the vendor
	// sweep — an `is_upgrade` mention (or a check placed after the rm) would
	// delete the NEW package's units during apt/dnf upgrade. Assert ordering,
	// not presence.
	gateIdx := strings.Index(script, `is_upgrade "${1:-}"`)
	sweepIdx := strings.Index(script, "rm -f \"$unit\"")
	if gateIdx < 0 || sweepIdx < 0 || gateIdx > sweepIdx {
		t.Fatalf("postremove.sh must check is_upgrade before sweeping vendor units:\n%s", script)
	}
	exitIdx := strings.Index(script[gateIdx:sweepIdx], "exit 0")
	if exitIdx < 0 {
		t.Fatalf("postremove.sh must exit 0 on upgrade before the vendor sweep:\n%s", script)
	}
	if strings.Contains(script, "/etc/systemd/system/veil") && strings.Contains(script, "rm -f \"$dir\"/veil") {
		// /etc units belong to `veil install`, not to the package.
		t.Fatal("postremove.sh must not remove operator units under /etc/systemd/system")
	}
}

// extractUpgradeGate pulls the is_upgrade() function body out of a packaging
// maintainer script, so a test can execute the real classification instead of
// grepping for a case list that may have gone stale (#1451).
func extractUpgradeGate(t *testing.T, path string) string {
	t.Helper()
	return extractShellFunc(t, path, "is_upgrade")
}

// extractShellFunc returns a shell function defined at column 0 in path, so a
// test can execute the real classification instead of grepping for a case
// list that may have gone stale (#1451, #1463).
func extractShellFunc(t *testing.T, path, name string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n")
	start := -1
	for i, line := range lines {
		if line == name+"() {" {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("%s: %s() not found at column 0", path, name)
	}
	for j := start + 1; j < len(lines); j++ {
		if lines[j] == "}" {
			return strings.Join(lines[start:j+1], "\n")
		}
	}
	t.Fatalf("%s: %s() is not terminated", path, name)
	return ""
}

// runUpgradeGate executes the extracted shell function for one argument and
// reports whether it classifies the argument as an upgrade.
func runUpgradeGate(t *testing.T, gate, arg string) bool {
	t.Helper()
	script := filepath.Join(t.TempDir(), "gate.sh")
	if err := os.WriteFile(script, []byte(gate+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Same interpreter the maintainer scripts declare (#!/bin/sh).
	cmd := exec.Command("/bin/sh", "-c",
		`. "$1"; if is_upgrade "$2"; then echo upgrade; else echo remove; fi`, "sh", script, arg)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("running the upgrade gate with %q: %v\n%s", arg, err, out)
	}
	return strings.TrimSpace(string(out)) == "upgrade"
}

// TestPackageUpgradeGateCoversAbortedOperations locks issue #1451: dpkg calls
// postrm with `abort-upgrade` after a failed upgrade unpack, and the old
// version stays installed. postremove.sh treated that as a real removal and
// deleted every packaged unit plus the QUIC sysctl drop-in, so a transient
// upgrade failure became a silent outage. abort-remove/abort-deconfigure mean
// dpkg rolled the package back for the same reason: the files stay.
func TestPackageUpgradeGateCoversAbortedOperations(t *testing.T) {
	// Package or files stay installed: upgrade in flight, or an aborted and
	// rolled-back remove/deconfigure/upgrade.
	keepArgs := []string{
		"upgrade", "deconfigure", "failed-upgrade",
		"abort-upgrade", "abort-remove", "abort-deconfigure",
		// RPM %postun leftover count: a positive integer means an instance stays.
		"1", "2",
	}
	// The package really goes away; the cleanup branches must still run.
	removeArgs := []string{"remove", "purge", "disappear", "", "0"}
	for _, path := range []string{
		"../../packaging/scripts/preremove.sh",
		"../../packaging/scripts/postremove.sh",
	} {
		gate := extractUpgradeGate(t, path)
		for _, arg := range keepArgs {
			if !runUpgradeGate(t, gate, arg) {
				t.Errorf("%s: is_upgrade %q = false, want true — files must be kept",
					filepath.Base(path), arg)
			}
		}
		for _, arg := range removeArgs {
			if runUpgradeGate(t, gate, arg) {
				t.Errorf("%s: is_upgrade %q = true, want false — cleanup must run",
					filepath.Base(path), arg)
			}
		}
	}
}

// runShellFunc executes one extracted shell function with a single argument
// under the interpreter the maintainer scripts declare, returning its stdout.
func runShellFunc(t *testing.T, fn, name, arg string) string {
	t.Helper()
	script := filepath.Join(t.TempDir(), "func.sh")
	if err := os.WriteFile(script, []byte(fn+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", "-c", `. "$1"; "$2" "$3"`, "sh", script, name, arg)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("running %s with %q: %v\n%s", name, arg, err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestPostinstallSkipsInstallBodyOnAbortedOperations locks #1463: dpkg runs
// postinst as abort-upgrade/abort-remove/abort-deconfigure/triggered when it
// rolls an operation back or processes triggers. The script used to run its
// whole install body for those arguments — `veil helper migrate` (which writes
// a fresh migration-backups snapshot holding copies of state.key/state.json)
// plus a try-restart of every managed unit — although dpkg asked for nothing.
// Only configure, a bare invocation (APK) and the RPM install counters run the
// install body; abort-remove only restarts the units preremove stopped.
func TestPostinstallSkipsInstallBodyOnAbortedOperations(t *testing.T) {
	const script = "../../packaging/scripts/postinstall.sh"
	fn := extractShellFunc(t, script, "postinstall_mode")
	for _, tc := range []struct {
		arg  string
		want string
	}{
		{"configure", "install"},
		{"configure 0.7.3", "install"}, // $1 only; Debian passes the old version as $2
		{"", "install"},
		{"1", "install"},
		{"2", "install"},
		{"abort-remove", "restart"},
		{"abort-upgrade", "skip"},
		{"abort-deconfigure", "skip"},
		{"triggered", "skip"},
		{"unexpected-argument", "skip"},
		// APK passes the package version as $1 (apk-tools run_script); on an
		// upgrade $1 is the new version and $2 the old one. Missing these
		// skipped account creation on every APK install.
		{"0.0.1", "install"},
		{"1.2.3-r0", "install"},
		{"1.0.0+cvs20240101", "install"},
		{"0", "skip"},
	} {
		want := tc.want
		if strings.Contains(tc.arg, " ") {
			// A multi-word entry just documents the $1-only contract; the
			// function receives the first word, as dpkg invokes it.
			tc.arg = strings.Fields(tc.arg)[0]
		}
		if got := runShellFunc(t, fn, "postinstall_mode", tc.arg); got != want {
			t.Errorf("postinstall_mode %q = %q, want %q", tc.arg, got, want)
		}
	}

	// The gate must run before the first mutating command, not merely exist.
	body, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	code := stripHashComments(t, strings.ReplaceAll(string(body), "\r\n", "\n"))
	gate := strings.Index(code, `MODE="$(postinstall_mode`)
	if gate < 0 {
		t.Fatalf("postinstall.sh does not read its maintainer-script argument:\n%s", code)
	}
	if !strings.Contains(code[gate:], `if [ "$MODE" = install ]; then`) {
		t.Fatal("postinstall.sh must guard the install body on the resolved mode")
	}
	for _, mutation := range []string{
		"\nensure_system_account veil\n",
		"/usr/local/bin/veil helper migrate",
		"systemctl try-restart ",
	} {
		if idx := strings.Index(code, mutation); idx >= 0 && idx < gate {
			t.Fatalf("postinstall.sh runs %q before the argument gate", strings.TrimSpace(mutation))
		}
	}
	// abort-remove must not re-run the migration, but must start the units
	// preremove stopped (try-restart is a no-op for stopped units) and restore
	// the helper socket preremove disabled.
	if !strings.Contains(code, `restart) start_managed_units ;;`) {
		t.Fatal("postinstall.sh must start the units preremove stopped on abort-remove")
	}
}

// TestPostinstallAbortRemoveStartsUnitsPreremoveStopped executes both systemd
// paths against a fake systemctl. configure must try-restart the units it
// replaces (they are still running), while abort-remove runs after preremove
// stopped and disabled them: systemctl(1) defines try-restart as doing nothing
// to a unit that is not running, so that path must use start, and it must
// re-enable the helper socket preremove disabled — otherwise a rolled-back
// removal leaves a package dpkg calls installed with a dead privileged helper.
func TestPostinstallAbortRemoveStartsUnitsPreremoveStopped(t *testing.T) {
	const script = "../../packaging/scripts/postinstall.sh"
	units := "veil.service veil-helper.service veil-helper.socket veil-caddy.service veil-mieru.service veil-warp.service veil-backup.service veil-backup.timer"
	for _, tc := range []struct {
		fn   string
		verb string
	}{
		{"restart_managed_units", "try-restart"},
		{"start_managed_units", "start"},
	} {
		t.Run(tc.fn, func(t *testing.T) {
			calls := runShellFuncStubbed(t, extractShellFunc(t, script, tc.fn), tc.fn)
			joined := "\n" + strings.Join(calls, "\n") + "\n"
			for _, want := range []string{
				"daemon-reload",
				"enable veil-helper.socket",
				tc.verb + " " + units,
				tc.verb + " veil-hysteria2@node1.service",
			} {
				if !strings.Contains(joined, "\n"+want+"\n") {
					t.Errorf("%s must call `systemctl %s`; got calls:\n%s", tc.fn, want, strings.Join(calls, "\n"))
				}
			}
			other := "start"
			if tc.verb == "start" {
				other = "try-restart"
			}
			for _, call := range calls {
				if strings.HasPrefix(call, other+" ") {
					t.Errorf("%s must not use `%s` (wrong verb for this path): %q", tc.fn, other, call)
				}
			}
		})
	}
}

// runShellFuncStubbed executes an extracted function with a fake systemctl
// first on PATH and returns the argv lines it recorded. The fake answers
// list-units with one live template instance so the per-instance loop runs.
func runShellFuncStubbed(t *testing.T, fn, name string) []string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "systemctl.log")
	stub := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$STUB_LOG\"\nif [ \"$1\" = list-units ]; then\n    echo 'veil-hysteria2@node1.service loaded active running'\nfi\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "systemctl"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "func.sh")
	if err := os.WriteFile(script, []byte(fn+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", "-c", `. "$1"; "$2"`, "sh", script, name)
	cmd.Env = append(os.Environ(),
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"STUB_LOG="+logPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("running %s: %v\n%s", name, err, out)
	}
	body, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("%s recorded no systemctl calls: %v", name, err)
	}
	return strings.Split(strings.TrimSpace(string(body)), "\n")
}
