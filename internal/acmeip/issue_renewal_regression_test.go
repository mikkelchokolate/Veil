package acmeip

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestRenewReloadCmdRepairsOwnershipBeforeRestart covers audit #120/#526/#527:
// acme.sh renewals rewrite tls.key as 0600 root:root, so the registered
// reloadcmd must restore group readability BEFORE restarting the panel or
// veil.service (User=veil) loses access to the key — and every step must fail
// closed so a renewal can never report success leaving an unreadable key.
func TestRenewReloadCmdRepairsOwnershipBeforeRestart(t *testing.T) {
	cmd := renewReloadCmd("/etc/veil/panel/tls.crt", "/etc/veil/panel/tls.key")

	chmodCert := strings.Index(cmd, "chmod 0644 '/etc/veil/panel/tls.crt'")
	chmodKey := strings.Index(cmd, "chmod 0640 '/etc/veil/panel/tls.key'")
	// Issue #760: the chgrp is a hard &&-joined step — a `|| chgrp veil`
	// fallback would mask a provisioning break and report a successful
	// renewal while veil-proxy units lose key readability.
	chgrp := strings.Index(cmd, "&& chgrp veil-proxy '/etc/veil/panel/tls.crt' '/etc/veil/panel/tls.key' &&")
	restart := strings.Index(cmd, "systemctl restart veil.service")
	for name, idx := range map[string]int{"chmod cert": chmodCert, "chmod key": chmodKey, "chgrp": chgrp, "restart": restart} {
		if idx < 0 {
			t.Fatalf("reloadcmd missing %s step: %q", name, cmd)
		}
	}
	if !(chmodCert < restart && chmodKey < restart && chgrp < restart) {
		t.Fatalf("permission repair must precede the restart: %q", cmd)
	}
	if !(chmodCert < chgrp && chmodKey < chgrp) {
		t.Fatalf("chgrp must follow chmod so the group gets the repaired modes: %q", cmd)
	}
	// No group fallback may rescue a failed veil-proxy chgrp. ("chgrp veil '"
	// cannot match "chgrp veil-proxy '" — the proxy name sits between.)
	if strings.Contains(cmd, "|| chgrp") {
		t.Fatalf("reloadcmd must not fall back to another group after a failed veil-proxy chgrp: %q", cmd)
	}
	if strings.Contains(cmd, "chgrp veil '") {
		t.Fatalf("reloadcmd chgrps the veil group, which veil-proxy units cannot read: %q", cmd)
	}
}

// TestRenewReloadCmdPinsDirOwnershipAndProtocolRestart covers audit #527: the
// cert directory must be re-chgrped (acme.sh can leave it 0700 root:root, which
// blocks group traverse), and the veil-proxy protocol units that serve the
// panel certificate (hysteria2) must be restarted — not just veil.service.
func TestRenewReloadCmdPinsDirOwnershipAndProtocolRestart(t *testing.T) {
	certPath := "/etc/veil/panel/tls.crt"
	cmd := renewReloadCmd(certPath, "/etc/veil/panel/tls.key")
	// The implementation derives the dir via filepath.Dir, so mirror that
	// rather than pinning a POSIX-only literal (Windows CI uses \separators).
	dir := filepath.Dir(certPath)

	for _, want := range []string{
		"chgrp veil-proxy '" + dir + "'",
		"chmod 0750 '" + dir + "'",
		"veil-hysteria2@*.service",
		// Issue #620: --plain keeps the status glyph out of awk's $1, and
		// --state=active keeps stopped/disabled/not-found instances out of
		// the loop so renewal cannot fail on — or revive — them.
		"list-units --plain --no-legend --state=active",
	} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("reloadcmd missing %q: %q", want, cmd)
		}
	}
	// Directory repair must run before the restarts as well.
	dirChgrp := strings.Index(cmd, "chgrp veil-proxy '"+dir+"'")
	dirChmod := strings.Index(cmd, "chmod 0750 '"+dir+"'")
	restart := strings.Index(cmd, "systemctl restart veil.service")
	if !(dirChgrp < restart && dirChmod < restart) {
		t.Fatalf("directory repair must precede the restart: %q", cmd)
	}
}

// TestRenewReloadCmdFailsClosed covers audit #526: no step of the renewal
// repair may be swallowed — a masked chgrp/restart failure used to leave
// tls.key unreadable or consumers running against stale material while the
// renewal reported success.
func TestRenewReloadCmdFailsClosed(t *testing.T) {
	cmd := renewReloadCmd("/etc/veil/panel/tls.crt", "/etc/veil/panel/tls.key")
	if strings.Contains(cmd, "|| true") {
		t.Fatalf("reloadcmd must not swallow failures with || true: %q", cmd)
	}
	if strings.Contains(cmd, "2>/dev/null") {
		t.Fatalf("reloadcmd must not hide command failures: %q", cmd)
	}
	// The veil.service restart is the last hard step before the optional
	// instance restarts; it must propagate its status.
	if !strings.Contains(cmd, "&& systemctl restart veil.service &&") {
		t.Fatalf("veil.service restart must be a hard step: %q", cmd)
	}
	// Instance restarts must fail the command too (exit 1 on restart failure).
	// try-restart (not restart) so an instance that went inactive after being
	// listed is skipped rather than revived (issue #620).
	if !strings.Contains(cmd, `systemctl try-restart "$u" || exit 1`) {
		t.Fatalf("protocol instance restart failures must propagate: %q", cmd)
	}
	if strings.Contains(cmd, `systemctl restart "$u"`) {
		t.Fatalf("protocol instances must use try-restart, not restart: %q", cmd)
	}
}

// TestShellQuoteEscapesSingleQuotes pins the reloadcmd quoting contract.
func TestShellQuoteEscapesSingleQuotes(t *testing.T) {
	if got := shellQuote("/etc/veil/panel/tls.key"); got != "'/etc/veil/panel/tls.key'" {
		t.Fatalf("shellQuote = %q", got)
	}
	if got := shellQuote("/o'clock/key"); got != `'/o'\''clock/key'` {
		t.Fatalf("shellQuote escape = %q", got)
	}
}
