package cli

import (
	"os"
	"strings"
	"testing"
)

// Regression for #354: /etc/veil/panel TLS is shared with the protocol units
// (User=veil-proxy), so the package migration path must group it veil-proxy —
// the same contract as /etc/veil/generated and /etc/veil/tls.
func TestPostinstallGroupsPanelTLSForProxyReaders(t *testing.T) {
	body, err := os.ReadFile("../../packaging/scripts/postinstall.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(string(body), "\r\n", "\n")
	// /etc/veil/panel is normalized inside the runtime-shared dir loop with the
	// same root:veil-proxy contract as generated/ and tls/.
	if !strings.Contains(script, "/etc/veil/panel; do") && !strings.Contains(script, "/etc/veil/panel ") {
		t.Fatalf("postinstall.sh must include /etc/veil/panel in the runtime-shared ownership pass:\n%s", script)
	}
	if !strings.Contains(script, `chown -R root:veil-proxy "$dir"`) {
		t.Fatalf("postinstall.sh must chown runtime-shared dirs to root:veil-proxy:\n%s", script)
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
	config := strings.ReplaceAll(string(body), "\r\n", "\n")
	for _, want := range []string{
		"apk:\n  scripts:\n    postupgrade: packaging/scripts/postinstall.sh",
		"postinstall: packaging/scripts/postinstall.sh",
	} {
		if !strings.Contains(config, want) {
			t.Fatalf("nfpm configuration missing APK upgrade hardening contract %q:\n%s", want, config)
		}
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
	if !strings.Contains(string(preremove), "stop_disable_unit veil-backup.service") {
		t.Fatal("preremove.sh must stop/disable veil-backup.service, not only the timer")
	}
	postinstall, err := os.ReadFile("../../packaging/scripts/postinstall.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(string(postinstall), "\r\n", "\n")
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
	pre := strings.ReplaceAll(string(preremove), "\r\n", "\n")
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
	post := strings.ReplaceAll(string(postremove), "\r\n", "\n")
	if !strings.Contains(post, `"$dir"/veil-caddy@*.service`) {
		t.Fatalf("postremove.sh must sweep legacy veil-caddy@* vendor unit files:\n%s", post)
	}
	uninstallSh, err := os.ReadFile("../../scripts/uninstall.sh")
	if err != nil {
		t.Fatal(err)
	}
	sh := strings.ReplaceAll(string(uninstallSh), "\r\n", "\n")
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
	script := strings.ReplaceAll(string(body), "\r\n", "\n")
	for _, want := range []string{
		"is_upgrade",
		"/lib/systemd/system",
		"/usr/lib/systemd/system",
		"/etc/sysctl.d/99-veil-quic.conf",
		"daemon-reload",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("postremove.sh missing %q:\n%s", want, script)
		}
	}
	if strings.Contains(script, "/etc/systemd/system/veil") && strings.Contains(script, "rm -f \"$dir\"/veil") {
		// /etc units belong to `veil install`, not to the package.
		t.Fatal("postremove.sh must not remove operator units under /etc/systemd/system")
	}
}
