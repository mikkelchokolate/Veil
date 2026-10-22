package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/renderer"
)

var deniedHostReport = renderer.EgressDNSReport{
	Nameservers:      []string{"172.31.0.2"},
	PinnedDoH:        "1.1.1.1:443",
	MieruNameservers: []string{"1.1.1.1", "1.0.0.1"},
}

// TestEgressResolvConfCommandWritesFallbackOnDeniedHost is the #1288 Mieru
// regression for the ExecStartPre=+ hook: on a denied host it must publish a
// world-readable resolv.conf naming the public fallback nameservers.
func TestEgressResolvConfCommandWritesFallbackOnDeniedHost(t *testing.T) {
	output := filepath.Join(t.TempDir(), "resolv.conf")
	old := egressResolvConfProbe
	t.Cleanup(func() { egressResolvConfProbe = old })
	egressResolvConfProbe = func() renderer.EgressDNSReport { return deniedHostReport }

	var out bytes.Buffer
	cmd := NewRootCommand("test")
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"egress-resolv-conf", "--output", output})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("egress-resolv-conf: %v\n%s", err, out.String())
	}
	body, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(body), "nameserver 1.1.1.1\nnameserver 1.0.0.1\n") {
		t.Fatalf("resolv.conf = %q", body)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(output)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o644 {
			t.Fatalf("resolv.conf mode = %v, want 0644 so mita (veil-mita) can read it", info.Mode().Perm())
		}
	}
	if !strings.Contains(out.String(), "1.1.1.1, 1.0.0.1") {
		t.Fatalf("output must name the fallback nameservers for the journal:\n%s", out.String())
	}
	entries, _ := os.ReadDir(filepath.Dir(output))
	if len(entries) != 1 {
		t.Fatalf("temp file left behind: %v", entries)
	}
}

// TestEgressResolvConfRemovesFallbackWhenHostResolverWorks: once the host
// DNS is fixed, the next start must drop the file so the bind is skipped.
// An unreadable resolv.conf also keeps the host file.
func TestEgressResolvConfRemovesFallbackWhenHostResolverWorks(t *testing.T) {
	for name, tc := range map[string]struct {
		report renderer.EgressDNSReport
		reason string
	}{
		"reachable":  {renderer.EgressDNSReport{Nameservers: []string{"127.0.0.53"}, Reachable: true}, "a host nameserver is reachable under the egress filter"},
		"unreadable": {renderer.EgressDNSReport{ReadError: "permission denied"}, "cannot read /etc/resolv.conf: permission denied"},
		"missing":    {renderer.EgressDNSReport{ResolvConfMissing: true, PinnedDoH: "1.1.1.1:443"}, "/etc/resolv.conf is missing"},
	} {
		report := tc.report
		t.Run(name, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "resolv.conf")
			if err := os.WriteFile(output, []byte("nameserver 1.1.1.1\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			var journal bytes.Buffer
			if err := writeEgressResolvConf(&journal, output, report); err != nil {
				t.Fatal(err)
			}
			// The journal line must say why, not claim a reachable
			// resolver on hosts where that is unknown or false.
			if !strings.Contains(journal.String(), "("+tc.reason+")") {
				t.Fatalf("journal line %q missing reason %q", journal.String(), tc.reason)
			}
			if _, err := os.Lstat(output); !os.IsNotExist(err) {
				t.Fatalf("fallback resolv.conf must be removed, Lstat err = %v", err)
			}
			// Absent already: still fine.
			if err := writeEgressResolvConf(&bytes.Buffer{}, output, report); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestEgressResolvConfDoesNotWriteThroughPlantedLink: the runtime directory
// is owned by veil-mita, and the hook runs as root. A link planted at the
// output path must be replaced, never followed.
func TestEgressResolvConfDoesNotWriteThroughPlantedLink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	dir := t.TempDir()
	victim := filepath.Join(dir, "victim")
	if err := os.WriteFile(victim, []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "resolv.conf")
	if err := os.Symlink(victim, output); err != nil {
		t.Fatal(err)
	}
	if err := writeEgressResolvConf(&bytes.Buffer{}, output, deniedHostReport); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(victim); string(got) != "secret\n" {
		t.Fatalf("hook wrote through the planted link: victim = %q", got)
	}
	info, err := os.Lstat(output)
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("output must be a regular file after publish: %v %v", info, err)
	}

	// Removal must unlink the link itself, not its target.
	if err := os.Remove(output); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, output); err != nil {
		t.Fatal(err)
	}
	if err := writeEgressResolvConf(&bytes.Buffer{}, output, renderer.EgressDNSReport{Reachable: true}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(victim); string(got) != "secret\n" {
		t.Fatalf("removal touched the link target: victim = %q", got)
	}
}

func TestEgressResolvConfRejectsRelativeOutput(t *testing.T) {
	for _, output := range []string{"resolv.conf", "/run/veil-mieru/../resolv.conf"} {
		if err := writeEgressResolvConf(&bytes.Buffer{}, output, deniedHostReport); err == nil {
			t.Fatalf("output %q must be rejected", output)
		}
	}
}
