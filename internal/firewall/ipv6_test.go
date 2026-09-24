package firewall

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	veilruntime "github.com/mikkelchokolate/Veil/internal/runtime"
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

func TestEnsureIPv6ManagedRepairsQuotedDisabledIPv6(t *testing.T) {
	for _, seed := range []string{"IPV6=\"no\"\n", "IPV6='no'\n", "IPV6=No\n"} {
		path := withUFWDefaultsFile(t, seed)
		if err := EnsureIPv6Managed(); err != nil {
			t.Fatalf("EnsureIPv6Managed(%q): %v", seed, err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read ufw defaults: %v", err)
		}
		// Assert the whole file, not just Contains(IPV6=yes): an append-only
		// "repair" would still contain the token while leaving the disabled
		// line in force.
		if string(data) != "IPV6=yes\n" {
			t.Fatalf("quoted/cased IPV6=no must be repaired in place, seed %q:\n%s", seed, data)
		}
	}
}

// Shell-legal trailing comments and other non-"yes" disable values UFW
// honors must not defeat the repair — the file is sourced by shell, so
// `IPV6=no # hardened` still disables IPv6 management.
func TestEnsureIPv6ManagedRepairsDisabledIPv6WithTrailingComment(t *testing.T) {
	seeds := map[string]string{
		"IPV6=no # hardened\n":        "IPV6=yes\n",
		"IPV6=\"no\" # comment\n":     "IPV6=yes\n",
		"IPV6='no'   # keep this\n":   "IPV6=yes\n",
		"IPV6=false\n":                "IPV6=yes\n",
		"IPV6=0\n":                    "IPV6=yes\n",
		"IPV6=\n":                     "IPV6=yes\n",
		"IPV6=yes#literal-not-yes\n":  "IPV6=yes\n",
		"IPV6=no\t# tabbed comment\n": "IPV6=yes\n",
	}
	for seed, want := range seeds {
		path := withUFWDefaultsFile(t, seed)
		if err := EnsureIPv6Managed(); err != nil {
			t.Fatalf("EnsureIPv6Managed(%q): %v", seed, err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read ufw defaults: %v", err)
		}
		if string(data) != want {
			t.Fatalf("disabled IPV6 must be repaired to exactly IPV6=yes, seed %q got:\n%s", seed, data)
		}
	}
	// A comment must not hide a preceding enabled sibling: the first
	// occurrence that disables is repaired; an already-enabled file is left
	// byte-identical (covered by the no-repair test below).
	path := withUFWDefaultsFile(t, "IPV6=no # off\nIPT_SYSCTL=/etc/ufw/sysctl.conf\n")
	if err := EnsureIPv6Managed(); err != nil {
		t.Fatalf("EnsureIPv6Managed: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read ufw defaults: %v", err)
	}
	want := "IPV6=yes\nIPT_SYSCTL=/etc/ufw/sysctl.conf\n"
	if string(data) != want {
		t.Fatalf("repair must preserve sibling lines, got:\n%s", data)
	}
}

func TestEnsureIPv6ManagedLeavesEnabledAndCommentedAlone(t *testing.T) {
	for _, content := range []string{
		"IPV6=yes\n",
		"IPV6=yes # managed\n",
		"IPV6=\"yes\"\n",
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

// ipv6ProbeRunner wraps safeUFWModel and records, at the moment each real
// (non-dry-run) allow mutation runs, whether /etc/default/ufw already reads
// IPV6=yes — proving the repair precedes staging rather than only showing up
// in the final file state.
type ipv6ProbeRunner struct {
	inner             *safeUFWModel
	defaultsPath      string
	allowCalls        int
	allowBeforeRepair bool
}

func (r *ipv6ProbeRunner) Run(input veilruntime.RuntimeCommandInput) veilruntime.RuntimeCommandOutput {
	out := r.inner.Run(input)
	cmd := input.Command
	if len(cmd) >= 2 && cmd[0] == "ufw" && cmd[1] == "allow" {
		r.allowCalls++
		data, err := os.ReadFile(r.defaultsPath)
		if err != nil || !strings.Contains(string(data), "IPV6=yes") {
			r.allowBeforeRepair = true
		}
	}
	return out
}

func TestApplySafelyRepairsIPv6BeforeStagingRules(t *testing.T) {
	path := withUFWDefaultsFile(t, "IPV6=no\n")
	runner := &ipv6ProbeRunner{inner: &safeUFWModel{rules: map[string]string{}}, defaultsPath: path}
	applier := NewUFWApplierWithRunner(runner)
	err := applier.ApplySafely([]Rule{
		{Command: "ufw", Args: []string{"allow", "22/tcp", "comment", "ssh management"}},
		{Command: "ufw", Args: []string{"allow", "4315/udp", "comment", "Veil Hysteria2"}},
	})
	if err != nil {
		t.Fatalf("ApplySafely: %v", err)
	}
	if runner.allowCalls != 2 {
		t.Fatalf("expected 2 allow mutations, got %d", runner.allowCalls)
	}
	if runner.allowBeforeRepair {
		t.Fatal("a ufw allow ran before IPV6=yes repair — rules staged without v6 twins")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read ufw defaults: %v", err)
	}
	if string(data) != "IPV6=yes\n" {
		t.Fatalf("IPV6=no must be repaired in place before rules install v6 twins:\n%s", data)
	}
}
