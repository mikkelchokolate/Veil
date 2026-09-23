package firewall

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withUFWDefaultsFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ufw")
	if content != "" {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("seed ufw defaults: %v", err)
		}
	}
	prev := ufwDefaultsPath
	ufwDefaultsPath = path
	t.Cleanup(func() { ufwDefaultsPath = prev })
	return path
}

func TestEnsureIPv6ManagedRepairsDisabledIPv6(t *testing.T) {
	path := withUFWDefaultsFile(t, "# ufw defaults\nIPV6=no\nIPT_SYSCTL=/etc/ufw/sysctl.conf\n")
	if err := EnsureIPv6Managed(); err != nil {
		t.Fatalf("EnsureIPv6Managed: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read ufw defaults: %v", err)
	}
	if !strings.Contains(string(data), "IPV6=yes") {
		t.Fatalf("expected IPV6=yes repair, got:\n%s", data)
	}
	if strings.Contains(string(data), "IPV6=no") {
		t.Fatalf("IPV6=no survived repair:\n%s", data)
	}
}

func TestEnsureIPv6ManagedLeavesEnabledAndCommentedAlone(t *testing.T) {
	for _, content := range []string{
		"IPV6=yes\n",
		"# IPV6=no\nIPV6=yes\n",
		"# only a commented IPV6=no\n",
	} {
		path := withUFWDefaultsFile(t, content)
		if err := EnsureIPv6Managed(); err != nil {
			t.Fatalf("EnsureIPv6Managed(%q): %v", content, err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read ufw defaults: %v", err)
		}
		if string(data) != content {
			t.Fatalf("file rewritten without need:\n%s", data)
		}
	}
}

func TestEnsureIPv6ManagedMissingFileIsNoop(t *testing.T) {
	withUFWDefaultsFile(t, "")
	if err := EnsureIPv6Managed(); err != nil {
		t.Fatalf("missing defaults file must be a no-op: %v", err)
	}
}

func TestApplySafelyRepairsIPv6BeforeStagingRules(t *testing.T) {
	path := withUFWDefaultsFile(t, "IPV6=no\n")
	runner := &safeUFWModel{rules: map[string]string{}}
	applier := NewUFWApplierWithRunner(runner)
	err := applier.ApplySafely([]Rule{
		{Command: "ufw", Args: []string{"allow", "22/tcp", "comment", "ssh management"}},
		{Command: "ufw", Args: []string{"allow", "4315/udp", "comment", "Veil Hysteria2"}},
	})
	if err != nil {
		t.Fatalf("ApplySafely: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read ufw defaults: %v", err)
	}
	if !strings.Contains(string(data), "IPV6=yes") {
		t.Fatalf("IPV6=no must be repaired before rules install v6 twins:\n%s", data)
	}
}
