package acmeip

import (
	"strings"
	"testing"
)

// TestRenewReloadCmdRepairsOwnershipBeforeRestart covers audit #120: acme.sh
// renewals rewrite tls.key as 0600 root:root, so the registered reloadcmd must
// restore group readability BEFORE restarting the panel or veil.service
// (User=veil) loses access to the key.
func TestRenewReloadCmdRepairsOwnershipBeforeRestart(t *testing.T) {
	cmd := renewReloadCmd("/etc/veil/panel/tls.crt", "/etc/veil/panel/tls.key")

	chmodCert := strings.Index(cmd, "chmod 0644 '/etc/veil/panel/tls.crt'")
	chmodKey := strings.Index(cmd, "chmod 0640 '/etc/veil/panel/tls.key'")
	chgrp := strings.Index(cmd, "chgrp veil '/etc/veil/panel/tls.crt' '/etc/veil/panel/tls.key'")
	restart := strings.Index(cmd, "systemctl restart veil")
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
