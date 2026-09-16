package cli

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/acmeip"
	"github.com/mikkelchokolate/Veil/internal/firewall"
	"github.com/mikkelchokolate/Veil/internal/hostaccess"
	"github.com/mikkelchokolate/Veil/internal/installer"
	"github.com/mikkelchokolate/Veil/internal/service"
)

// TestDirectInstallCreatesVeilGroupBeforeIssuingIPCert covers audit #120:
// fixCertOwnership chgrps the installed key to the veil group, so
// hostaccess.Prepare (which creates that group) must run before the ACME IP
// certificate is issued — not after.
func TestDirectInstallCreatesVeilGroupBeforeIssuingIPCert(t *testing.T) {
	withMockedInstallRuntimes(t)
	oldApply := installApplyFunc
	oldSystemd := installSystemdRunFunc
	oldExecutable := installExecutableFunc
	oldPrepareHost := installPrepareHostFunc
	oldFirewall := installFirewallApplyFunc
	oldIssue := leIPCertIssueFunc

	var events []string
	installApplyFunc = func(profile installer.RURecommendedProfile, paths installer.ApplyPaths) (installer.ApplyResult, error) {
		return installer.ApplyResult{BackupID: "backup-order", WrittenFiles: []string{"/etc/veil/veil.env"}}, nil
	}
	installSystemdRunFunc = func(actions []service.SystemdAction) error { return nil }
	installExecutableFunc = func() (string, error) { return "/opt/veil/bin/veil", nil }
	installFirewallApplyFunc = func(rules []firewall.Rule) error { return nil }
	installPrepareHostFunc = func(paths hostaccess.Paths) error {
		events = append(events, "prepare")
		return nil
	}
	leIPCertIssueFunc = func(ctx context.Context, opts acmeip.IssueOptions) (acmeip.IssuedCert, error) {
		events = append(events, "issue")
		if err := os.WriteFile(opts.CertPath, []byte("LE-CERT"), 0o644); err != nil {
			t.Fatalf("write fake cert: %v", err)
		}
		if err := os.WriteFile(opts.KeyPath, []byte("LE-KEY"), 0o640); err != nil {
			t.Fatalf("write fake key: %v", err)
		}
		return acmeip.IssuedCert{CertPath: opts.CertPath, KeyPath: opts.KeyPath}, nil
	}
	t.Cleanup(func() {
		installApplyFunc = oldApply
		installSystemdRunFunc = oldSystemd
		installExecutableFunc = oldExecutable
		installPrepareHostFunc = oldPrepareHost
		installFirewallApplyFunc = oldFirewall
		leIPCertIssueFunc = oldIssue
	})

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	profile := installer.RURecommendedProfile{
		Username:    "veil",
		Password:    "test-password",
		WebBasePath: "/panel/",
		PanelListen: "0.0.0.0:3000",
		PanelAccess: "direct",
	}
	err := applyRURecommendedInstall(cmd, profile, ruRecommendedInstallOptions{
		EtcDir:       t.TempDir(),
		VarDir:       t.TempDir(),
		PanelAccess:  "direct",
		PanelPort:    3000,
		LEIPCert:     true,
		LEIPCertPort: 80,
		PublicIP:     "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("applyRURecommendedInstall: %v", err)
	}
	prepareIdx, issueIdx := -1, -1
	for i, e := range events {
		if e == "prepare" {
			prepareIdx = i
		}
		if e == "issue" {
			issueIdx = i
		}
	}
	if prepareIdx < 0 || issueIdx < 0 || prepareIdx > issueIdx {
		t.Fatalf("veil group must exist before cert issuance, events=%v", events)
	}
}
