package cli

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/acmeip"
	"github.com/mikkelchokolate/Veil/internal/firewall"
	"github.com/mikkelchokolate/Veil/internal/hostaccess"
	"github.com/mikkelchokolate/Veil/internal/installer"
	"github.com/mikkelchokolate/Veil/internal/service"
)

func TestDirectInstallOpensACMEPortBeforeIssuingCertificate(t *testing.T) {
	withMockedInstallRuntimes(t)
	oldApply := installApplyFunc
	oldSystemd := installSystemdRunFunc
	oldExecutable := installExecutableFunc
	oldPrepareHost := installPrepareHostFunc
	oldFirewall := installFirewallApplyFunc
	oldIssue := leIPCertIssueFunc

	var events []string
	var acmeRuleStaged bool
	installApplyFunc = func(profile installer.RURecommendedProfile, paths installer.ApplyPaths) (installer.ApplyResult, error) {
		return installer.ApplyResult{BackupID: "backup-acme-order", WrittenFiles: []string{"/etc/veil/veil.env"}}, nil
	}
	installSystemdRunFunc = func(actions []service.SystemdAction) error { return nil }
	installExecutableFunc = func() (string, error) { return "/opt/veil/bin/veil", nil }
	installPrepareHostFunc = func(paths hostaccess.Paths) error { return nil }
	installFirewallApplyFunc = func(rules []firewall.Rule) error {
		events = append(events, "firewall")
		for _, rule := range rules {
			joined := strings.Join(rule.Args, " ")
			if strings.Contains(joined, "80/tcp") && strings.Contains(joined, "ACME HTTP-01") {
				acmeRuleStaged = true
			}
		}
		return nil
	}
	leIPCertIssueFunc = func(ctx context.Context, opts acmeip.IssueOptions) (acmeip.IssuedCert, error) {
		if !acmeRuleStaged {
			t.Fatal("HTTP-01 issuance ran before the planned ACME firewall rule was applied")
		}
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
	if !acmeRuleStaged {
		t.Fatal("expected the ACME HTTP-01 firewall rule to be applied")
	}
	if len(events) < 2 || events[0] != "firewall" || events[1] != "issue" {
		t.Fatalf("expected firewall then issue, got %v", events)
	}
}
