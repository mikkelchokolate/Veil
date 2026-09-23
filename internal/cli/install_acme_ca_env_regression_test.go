package cli

import (
	"context"
	"errors"
	"net"
	"os"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/acmeip"
	"github.com/mikkelchokolate/Veil/internal/installer"
)

// install-acceptance drives the controlled-CA leg with VEIL_ACME_CA_URL /
// VEIL_ACME_INSECURE. The env plumbing into IssueOptions regressed silently
// once already — the CI script exported the variables but nothing read them,
// so issuance hit public Let's Encrypt. Pin the contract.
// stubPublicIPFamilyDetect keeps the best-effort second-family auto-detect
// probe (#665) off the network in unit tests.
func stubPublicIPFamilyDetect(t *testing.T, results map[string]net.IP) {
	t.Helper()
	old := installPublicIPFamilyDetectFunc
	installPublicIPFamilyDetectFunc = func(ctx context.Context, endpoints []string, network string) (net.IP, error) {
		if ip, ok := results[network]; ok {
			return ip, nil
		}
		return nil, errors.New("no public address for " + network)
	}
	t.Cleanup(func() { installPublicIPFamilyDetectFunc = old })
}

func TestIssueLEIPCertForProfileHonorsAcmeCAEnv(t *testing.T) {
	oldIssue := leIPCertIssueFunc
	t.Cleanup(func() { leIPCertIssueFunc = oldIssue })
	stubPublicIPFamilyDetect(t, nil)

	var got acmeip.IssueOptions
	leIPCertIssueFunc = func(ctx context.Context, opts acmeip.IssueOptions) (acmeip.IssuedCert, error) {
		got = opts
		if err := os.WriteFile(opts.CertPath, []byte("CA-CERT"), 0o644); err != nil {
			t.Fatalf("write fake cert: %v", err)
		}
		if err := os.WriteFile(opts.KeyPath, []byte("CA-KEY"), 0o640); err != nil {
			t.Fatalf("write fake key: %v", err)
		}
		return acmeip.IssuedCert{CertPath: opts.CertPath, KeyPath: opts.KeyPath}, nil
	}

	t.Setenv("VEIL_ACME_CA_URL", "https://127.0.0.1:14000/dir")
	t.Setenv("VEIL_ACME_INSECURE", "1")

	tempEtc := t.TempDir()
	profile := installer.RURecommendedProfile{Domain: "127.0.0.1"}
	resolvedIP := net.ParseIP("127.0.0.1")
	if err := issueLEIPCertForProfile(context.Background(), &profile, ruRecommendedInstallOptions{
		EtcDir: tempEtc,
	}, resolvedIP); err != nil {
		t.Fatalf("issueLEIPCertForProfile: %v", err)
	}
	if got.CAServer != "https://127.0.0.1:14000/dir" {
		t.Fatalf("CAServer = %q, want controlled CA from VEIL_ACME_CA_URL", got.CAServer)
	}
	if !got.Insecure {
		t.Fatal("Insecure = false, want true from VEIL_ACME_INSECURE")
	}
}

func TestIssueLEIPCertForProfileDefaultsToLetsEncrypt(t *testing.T) {
	oldIssue := leIPCertIssueFunc
	t.Cleanup(func() { leIPCertIssueFunc = oldIssue })
	stubPublicIPFamilyDetect(t, nil)
	t.Setenv("VEIL_ACME_CA_URL", "")
	t.Setenv("VEIL_ACME_INSECURE", "")

	var got acmeip.IssueOptions
	leIPCertIssueFunc = func(ctx context.Context, opts acmeip.IssueOptions) (acmeip.IssuedCert, error) {
		got = opts
		if err := os.WriteFile(opts.CertPath, []byte("CA-CERT"), 0o644); err != nil {
			t.Fatalf("write fake cert: %v", err)
		}
		if err := os.WriteFile(opts.KeyPath, []byte("CA-KEY"), 0o640); err != nil {
			t.Fatalf("write fake key: %v", err)
		}
		return acmeip.IssuedCert{CertPath: opts.CertPath, KeyPath: opts.KeyPath}, nil
	}

	tempEtc := t.TempDir()
	profile := installer.RURecommendedProfile{Domain: "127.0.0.1"}
	resolvedIP := net.ParseIP("127.0.0.1")
	if err := issueLEIPCertForProfile(context.Background(), &profile, ruRecommendedInstallOptions{
		EtcDir: tempEtc,
	}, resolvedIP); err != nil {
		t.Fatalf("issueLEIPCertForProfile: %v", err)
	}
	if got.CAServer != "" {
		t.Fatalf("CAServer = %q, want empty (Let's Encrypt default)", got.CAServer)
	}
	if got.Insecure {
		t.Fatal("Insecure = true, want false without VEIL_ACME_INSECURE")
	}
}

// Issue #665: an auto-detected IPv4 must not leave the host's public IPv6
// uncovered — the best-effort tcp6 probe fills PublicIPv6 so the issued SANs
// span both families.
func TestIssueLEIPCertForProfileAutoDetectCoversBothFamilies(t *testing.T) {
	oldIssue := leIPCertIssueFunc
	t.Cleanup(func() { leIPCertIssueFunc = oldIssue })
	stubPublicIPFamilyDetect(t, map[string]net.IP{"tcp6": net.ParseIP("2001:db8::5")})

	var got acmeip.IssueOptions
	leIPCertIssueFunc = func(ctx context.Context, opts acmeip.IssueOptions) (acmeip.IssuedCert, error) {
		got = opts
		if err := os.WriteFile(opts.CertPath, []byte("LE-CERT"), 0o644); err != nil {
			t.Fatalf("write fake cert: %v", err)
		}
		if err := os.WriteFile(opts.KeyPath, []byte("LE-KEY"), 0o640); err != nil {
			t.Fatalf("write fake key: %v", err)
		}
		return acmeip.IssuedCert{CertPath: opts.CertPath, KeyPath: opts.KeyPath}, nil
	}

	tempEtc := t.TempDir()
	profile := installer.RURecommendedProfile{Domain: "203.0.113.9"}
	resolvedIP := net.ParseIP("203.0.113.9")
	if err := issueLEIPCertForProfile(context.Background(), &profile, ruRecommendedInstallOptions{
		EtcDir: tempEtc,
	}, resolvedIP); err != nil {
		t.Fatalf("issueLEIPCertForProfile: %v", err)
	}
	if got.PublicIPv4 != "203.0.113.9" {
		t.Fatalf("PublicIPv4 = %q, want 203.0.113.9", got.PublicIPv4)
	}
	if got.PublicIPv6 != "2001:db8::5" {
		t.Fatalf("PublicIPv6 = %q, want probed 2001:db8::5", got.PublicIPv6)
	}
}

// Issue #665: an explicit IPv6 --public-ip must populate PublicIPv6, never
// PublicIPv4, and must not trigger the other-family probe.
func TestIssueLEIPCertForProfileExplicitIPv6(t *testing.T) {
	oldIssue := leIPCertIssueFunc
	t.Cleanup(func() { leIPCertIssueFunc = oldIssue })
	probed := false
	oldDetect := installPublicIPFamilyDetectFunc
	installPublicIPFamilyDetectFunc = func(ctx context.Context, endpoints []string, network string) (net.IP, error) {
		probed = true
		return nil, errors.New("must not probe for explicit public IP")
	}
	t.Cleanup(func() { installPublicIPFamilyDetectFunc = oldDetect })

	var got acmeip.IssueOptions
	leIPCertIssueFunc = func(ctx context.Context, opts acmeip.IssueOptions) (acmeip.IssuedCert, error) {
		got = opts
		if err := os.WriteFile(opts.CertPath, []byte("LE-CERT"), 0o644); err != nil {
			t.Fatalf("write fake cert: %v", err)
		}
		if err := os.WriteFile(opts.KeyPath, []byte("LE-KEY"), 0o640); err != nil {
			t.Fatalf("write fake key: %v", err)
		}
		return acmeip.IssuedCert{CertPath: opts.CertPath, KeyPath: opts.KeyPath}, nil
	}

	tempEtc := t.TempDir()
	profile := installer.RURecommendedProfile{Domain: "2001:db8::7"}
	if err := issueLEIPCertForProfile(context.Background(), &profile, ruRecommendedInstallOptions{
		EtcDir:   tempEtc,
		PublicIP: "2001:db8::7",
	}, net.ParseIP("2001:db8::7")); err != nil {
		t.Fatalf("issueLEIPCertForProfile: %v", err)
	}
	if got.PublicIPv4 != "" {
		t.Fatalf("PublicIPv4 = %q, want empty for IPv6-only host", got.PublicIPv4)
	}
	if got.PublicIPv6 != "2001:db8::7" {
		t.Fatalf("PublicIPv6 = %q, want 2001:db8::7", got.PublicIPv6)
	}
	if probed {
		t.Fatal("family probe must not run for an explicit --public-ip")
	}
}
