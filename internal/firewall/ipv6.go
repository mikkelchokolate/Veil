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
		if !strings.EqualFold(strings.Trim(strings.TrimSpace(value), `"'`), "no") {
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
