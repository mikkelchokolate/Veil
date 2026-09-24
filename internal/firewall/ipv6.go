package firewall

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// ufwDefaultsPath is the UFW system configuration that selects whether UFW
// manages IPv6 at all. Overridable in tests.
var ufwDefaultsPath = "/etc/default/ufw"

// EnsureIPv6Managed repairs /etc/default/ufw so UFW manages IPv6 before Veil
// relies on IPv4 rules installing their (v6) twins. A host hardened with
// IPV6=no would otherwise accept the v4 allow while leaving dual-stack panel
// and inbound listeners reachable through unmanaged ip6tables defaults.
// Once Veil manages the firewall it owns this key; an absent or commented
// setting already resolves to the UFW default (IPv6 managed).
func EnsureIPv6Managed() error {
	data, err := os.ReadFile(ufwDefaultsPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read %s: %w", ufwDefaultsPath, err)
	}
	info, statErr := os.Stat(ufwDefaultsPath)
	if statErr != nil {
		return fmt.Errorf("stat %s: %w", ufwDefaultsPath, statErr)
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	repaired := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, value, ok := strings.Cut(trimmed, "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(key), "IPV6") {
			continue
		}
		if ufwIPv6Enabled(value) {
			continue
		}
		lines[i] = "IPV6=yes"
		repaired = true
	}
	if !repaired {
		return nil
	}
	content := strings.Join(lines, "\n")
	if err := os.WriteFile(ufwDefaultsPath, []byte(content), info.Mode().Perm()); err != nil {
		return fmt.Errorf("rewrite %s with IPV6=yes: %w", ufwDefaultsPath, err)
	}
	return nil
}

// ufwIPv6Enabled reports whether the right-hand side of an IPV6= assignment
// leaves UFW managing IPv6. /etc/default/ufw is sourced by shell (and read by
// ufw with a "yes" check): only an effective value of "yes" enables
// management, so any other value — `no`, `false`, `0`, empty — is a disable
// that must be repaired. Shell-legal trailing comments (`IPV6=no # hardened`)
// and quoting (`IPV6="no"`) must not defeat the check.
func ufwIPv6Enabled(raw string) bool {
	return strings.EqualFold(ufwShellValue(raw), "yes")
}

// ufwShellValue extracts the effective value of a shell assignment's
// right-hand side: either a quoted string (content up to the closing quote)
// or a bare word terminated by whitespace or a `#` comment start.
func ufwShellValue(raw string) string {
	s := strings.TrimSpace(raw)
	if len(s) > 0 && (s[0] == '"' || s[0] == '\'') {
		if end := strings.IndexByte(s[1:], s[0]); end >= 0 {
			return s[1 : 1+end]
		}
		return s[1:] // unterminated quote — best effort
	}
	// A bare word ends at whitespace. A `#` starts a comment only at the
	// start of a word — inside a bare word it is literal (`IPV6=yes#foo`
	// assigns "yes#foo", which is still not "yes" and must be repaired).
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		s = s[:i]
	}
	if strings.HasPrefix(s, "#") {
		return ""
	}
	return s
}
