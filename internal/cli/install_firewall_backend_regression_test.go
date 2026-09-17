package cli

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/firewall"
	"github.com/mikkelchokolate/Veil/internal/hostaccess"
	"github.com/mikkelchokolate/Veil/internal/installer"
	"github.com/mikkelchokolate/Veil/internal/service"
	"github.com/spf13/cobra"
)

// stubFirewallBackendEnv installs controllable seams for the firewall
// dependency phase: the test runs as root, ufw presence is driven by a fake
// PATH map, and package-manager calls are recorded instead of executed.
func stubFirewallBackendEnv(t *testing.T, binaries map[string]bool) (calls *[]string) {
	t.Helper()
	recorded := []string{}

	oldEuid := installGeteuidFunc
	oldLookPath := execLookPath
	oldForeign := activeForeignFirewallFunc
	oldProvisionCmd := provisionUFWCommandFunc
	installGeteuidFunc = func() int { return 0 }
	execLookPath = func(name string) (string, error) {
		if binaries[name] {
			return "/usr/bin/" + name, nil
		}
		return "", errors.New("not found")
	}
	activeForeignFirewallFunc = func(context.Context) string { return "" }
	provisionUFWCommandFunc = func(_ context.Context, name string, args ...string) error {
		recorded = append(recorded, name+" "+strings.Join(args, " "))
		if name == "apt-get" && len(args) > 0 && args[0] == "install" {
			binaries["ufw"] = true
		}
		return nil
	}
	t.Cleanup(func() {
		installGeteuidFunc = oldEuid
		execLookPath = oldLookPath
		activeForeignFirewallFunc = oldForeign
		provisionUFWCommandFunc = oldProvisionCmd
	})
	return &recorded
}

func directInstallProfile() installer.RURecommendedProfile {
	return installer.RURecommendedProfile{PanelAccess: "direct", PanelListen: "0.0.0.0:2096"}
}

func TestInstallFirewallBackendSkipsWhenUFWPexists(t *testing.T) {
	calls := stubFirewallBackendEnv(t, map[string]bool{"ufw": true, "apt-get": true})
	if err := ensureInstallFirewallBackend(context.Background(), directInstallProfile(), ruRecommendedInstallOptions{PanelAccess: "direct"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("no package installs expected when ufw exists, got %v", *calls)
	}
}

func TestInstallFirewallBackendProvisionsUFWBeforeMutation(t *testing.T) {
	binaries := map[string]bool{"apt-get": true}
	calls := stubFirewallBackendEnv(t, binaries)
	if err := ensureInstallFirewallBackend(context.Background(), directInstallProfile(), ruRecommendedInstallOptions{PanelAccess: "direct"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"apt-get update", "apt-get install -y ufw"}
	if len(*calls) != len(want) {
		t.Fatalf("provision calls = %v, want %v", *calls, want)
	}
	for i := range want {
		if (*calls)[i] != want[i] {
			t.Fatalf("provision calls = %v, want %v", *calls, want)
		}
	}
}

func TestInstallFirewallBackendRefusesCompetingFirewall(t *testing.T) {
	calls := stubFirewallBackendEnv(t, map[string]bool{"apt-get": true})
	oldForeign := activeForeignFirewallFunc
	activeForeignFirewallFunc = func(context.Context) string { return "firewalld" }
	t.Cleanup(func() { activeForeignFirewallFunc = oldForeign })

	err := ensureInstallFirewallBackend(context.Background(), directInstallProfile(), ruRecommendedInstallOptions{PanelAccess: "direct"})
	if err == nil || !strings.Contains(err.Error(), "firewalld") {
		t.Fatalf("expected competing-firewall error, got %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("must not install ufw next to an active foreign firewall, got %v", *calls)
	}
}

func TestInstallFirewallBackendNoPlanNoop(t *testing.T) {
	calls := stubFirewallBackendEnv(t, map[string]bool{"apt-get": true})
	profile := installer.RURecommendedProfile{PanelAccess: "local", PanelListen: "127.0.0.1:2096"}
	if err := ensureInstallFirewallBackend(context.Background(), profile, ruRecommendedInstallOptions{PanelAccess: "local"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("local access has no firewall plan, got calls %v", *calls)
	}
}

func TestInstallFirewallBackendReportsMissingManager(t *testing.T) {
	stubFirewallBackendEnv(t, map[string]bool{})
	err := ensureInstallFirewallBackend(context.Background(), directInstallProfile(), ruRecommendedInstallOptions{PanelAccess: "direct"})
	if err == nil || !strings.Contains(err.Error(), "ufw") {
		t.Fatalf("expected actionable ufw error, got %v", err)
	}
}

func TestInstallFirewallBackendSkipsNonRoot(t *testing.T) {
	calls := stubFirewallBackendEnv(t, map[string]bool{"apt-get": true})
	installGeteuidFunc = func() int { return 1000 }
	if err := ensureInstallFirewallBackend(context.Background(), directInstallProfile(), ruRecommendedInstallOptions{PanelAccess: "direct"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("non-root install must not provision packages, got %v", *calls)
	}
}

func TestInstallFirewallBackendCheckRunsBeforeRuntimes(t *testing.T) {
	order := []string{}
	oldBackend := installEnsureFirewallBackendFunc
	oldRuntimes := installRuntimesFunc
	oldApply := installApplyFunc
	oldSystemd := installSystemdRunFunc
	oldExecutable := installExecutableFunc
	oldWait := installWaitPanelReadyFunc
	oldLookPath := commandLookPath
	oldPrepare := installPrepareHostFunc
	oldFirewallApply := installFirewallApplyFunc
	installEnsureFirewallBackendFunc = func(context.Context, installer.RURecommendedProfile, ruRecommendedInstallOptions) error {
		order = append(order, "firewall-backend")
		return nil
	}
	installFirewallApplyFunc = func([]firewall.Rule) error { return nil }
	installRuntimesFunc = func(cmd *cobra.Command, _ ruRecommendedInstallOptions) {
		order = append(order, "runtimes")
	}
	installApplyFunc = func(installer.RURecommendedProfile, installer.ApplyPaths) (installer.ApplyResult, error) {
		return installer.ApplyResult{}, nil
	}
	installSystemdRunFunc = func([]service.SystemdAction) error { return nil }
	installExecutableFunc = func() (string, error) { return "/usr/local/bin/veil", nil }
	installWaitPanelReadyFunc = func(*cobra.Command, installer.RURecommendedProfile, ruRecommendedInstallOptions) error {
		return nil
	}
	commandLookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	installPrepareHostFunc = func(hostaccess.Paths) error { return nil }
	t.Cleanup(func() {
		installEnsureFirewallBackendFunc = oldBackend
		installRuntimesFunc = oldRuntimes
		installApplyFunc = oldApply
		installSystemdRunFunc = oldSystemd
		installExecutableFunc = oldExecutable
		installWaitPanelReadyFunc = oldWait
		commandLookPath = oldLookPath
		installPrepareHostFunc = oldPrepare
		installFirewallApplyFunc = oldFirewallApply
	})

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	dir := t.TempDir()
	cmd.SetArgs([]string{
		"install",
		"--profile", "ru-recommended",
		"--panel-access", "caddy",
		"--domain", "example.com",
		"--email", "admin@example.com",
		"--etc-dir", filepath.Join(dir, "etc"),
		"--var-dir", filepath.Join(dir, "var"),
		"--systemd-dir", filepath.Join(dir, "systemd"),
		"--yes",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("install: %v\n%s", err, out.String())
	}
	if len(order) < 2 || order[0] != "firewall-backend" || order[1] != "runtimes" {
		t.Fatalf("firewall dependency phase must precede runtime downloads, got %v", order)
	}
}

// selfSignedCertPEM writes a certificate whose SANs mirror the panel's
// generated material for the readiness check.
func selfSignedCertPEM(t *testing.T, dns []string, ips []string) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "veil-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     dns,
	}
	for _, ip := range ips {
		if parsed := net.ParseIP(ip); parsed != nil {
			template.IPAddresses = append(template.IPAddresses, parsed)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "tls.crt")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadinessServerNamePrefersCoveredIdentity(t *testing.T) {
	// IP-certificate issuance names the public IP, not the configured domain.
	ipCert := selfSignedCertPEM(t, nil, []string{"192.0.2.20"})
	if got := readinessServerName(ipCert, "panel.example.com"); got != "192.0.2.20" {
		t.Fatalf("ip cert: serverName = %q, want the covered IP", got)
	}

	// Self-signed fallback generated before public-IP detection covers
	// loopback/interfaces, not the backfilled public domain.
	loopCert := selfSignedCertPEM(t, nil, []string{"127.0.0.1", "::1", "10.0.0.20"})
	if got := readinessServerName(loopCert, "192.0.2.20"); got != "127.0.0.1" {
		t.Fatalf("loopback cert: serverName = %q, want 127.0.0.1", got)
	}

	// A cert covering the configured domain keeps using it.
	dnsCert := selfSignedCertPEM(t, []string{"panel.example.com"}, nil)
	if got := readinessServerName(dnsCert, "panel.example.com"); got != "panel.example.com" {
		t.Fatalf("dns cert: serverName = %q", got)
	}

	// Missing cert material falls back to the previous contract.
	if got := readinessServerName(filepath.Join(t.TempDir(), "nope.crt"), "panel.example.com"); got != "panel.example.com" {
		t.Fatalf("missing cert: serverName = %q", got)
	}
	if got := readinessServerName(filepath.Join(t.TempDir(), "nope.crt"), ""); got != "localhost" {
		t.Fatalf("missing cert empty domain: serverName = %q", got)
	}
}
