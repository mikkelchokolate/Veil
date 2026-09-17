package cli

import (
	"context"
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
func TestIssueLEIPCertForProfileHonorsAcmeCAEnv(t *testing.T) {
	oldIssue := leIPCertIssueFunc
	t.Cleanup(func() { leIPCertIssueFunc = oldIssue })

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
