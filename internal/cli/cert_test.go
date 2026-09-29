package cli

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/privileged"
	"github.com/spf13/cobra"
)

// writeCLICertPEM writes a self-signed certificate with the given issuer org
// and expiry so `veil cert status` can be exercised against real PEM input.
func writeCLICertPEM(t *testing.T, path, issuerOrg string, notAfter time.Time, ips ...net.IP) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "veil-cert-test", Organization: []string{issuerOrg}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		t.Fatal(err)
	}
}

func certTestCommand(out *bytes.Buffer) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	return cmd
}

func TestCertStatusMissingCertificate(t *testing.T) {
	etc := t.TempDir()
	t.Setenv("VEIL_ETC_DIR", etc)
	t.Setenv("VEIL_TLS_CERT", "")
	t.Setenv("VEIL_TLS_KEY", "")

	out := &bytes.Buffer{}
	if err := runCertStatus(certTestCommand(out), false); err != nil {
		t.Fatalf("status must report, not fail, on a missing certificate: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "State:") || !strings.Contains(text, "Renewal:     required") {
		t.Fatalf("missing certificate must print an error state requiring renewal:\n%s", text)
	}
}

func TestCertStatusSelfSignedNeedsRenewal(t *testing.T) {
	etc := t.TempDir()
	t.Setenv("VEIL_ETC_DIR", etc)
	t.Setenv("VEIL_TLS_CERT", "")
	t.Setenv("VEIL_TLS_KEY", "")
	certPath := filepath.Join(etc, "panel", "tls.crt")
	writeCLICertPEM(t, certPath, "Veil Self-Signed", time.Now().Add(30*24*time.Hour), net.ParseIP("203.0.113.9"))

	out := &bytes.Buffer{}
	if err := runCertStatus(certTestCommand(out), false); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "Issuer:") || !strings.Contains(text, "Renewal:     required") {
		t.Fatalf("self-signed certificate must require renewal:\n%s", text)
	}
	if !strings.Contains(text, "203.0.113.9") {
		t.Fatalf("IP SAN must be surfaced in status output:\n%s", text)
	}
}

func TestCertStatusLetsEncryptOutsideWindow(t *testing.T) {
	etc := t.TempDir()
	t.Setenv("VEIL_ETC_DIR", etc)
	certPath := filepath.Join(etc, "panel", "tls.crt")
	writeCLICertPEM(t, certPath, "Let's Encrypt", time.Now().Add(5*24*time.Hour))

	out := &bytes.Buffer{}
	if err := runCertStatus(certTestCommand(out), false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Renewal:     not required") {
		t.Fatalf("healthy Let's Encrypt certificate must not require renewal:\n%s", out.String())
	}
}

func TestCertStatusJSON(t *testing.T) {
	etc := t.TempDir()
	t.Setenv("VEIL_ETC_DIR", etc)
	certPath := filepath.Join(etc, "panel", "tls.crt")
	writeCLICertPEM(t, certPath, "Let's Encrypt", time.Now().Add(48*time.Hour), net.ParseIP("2001:db8::7"))

	out := &bytes.Buffer{}
	if err := runCertStatus(certTestCommand(out), true); err != nil {
		t.Fatal(err)
	}
	var view certStatusView
	if err := json.Unmarshal(out.Bytes(), &view); err != nil {
		t.Fatalf("status --json must emit valid JSON: %v\n%s", err, out.String())
	}
	if !view.NeedsRenewal {
		t.Fatal("certificate inside the 72h window must report needsRenewal")
	}
	if view.RenewalWindowH != 72 {
		t.Fatalf("renewalWindowHours = %d, want 72", view.RenewalWindowH)
	}
	if len(view.IPAddresses) != 1 || view.IPAddresses[0] != "2001:db8::7" {
		t.Fatalf("IPv6 SAN lost from JSON view: %+v", view.IPAddresses)
	}
}

// stubCertRenew swaps the fencing and issuer seams so renew never touches
// the lease DB or the helper socket, and returns the captured request sink.
func stubCertRenew(t *testing.T, issuerErr error) (*privileged.IssueIPCertRequest, *int) {
	t.Helper()
	oldFence, oldIssuer := certRenewFence, certRenewIssuer
	calls := new(int)
	captured := new(privileged.IssueIPCertRequest)
	certRenewFence = func() (privileged.FenceToken, func(), error) {
		return privileged.FenceToken{Owner: "test", Generation: 7, OperationID: "cert-renew"}, func() {}, nil
	}
	certRenewIssuer = func(string) privileged.IPCertIssuer {
		return certRenewIssuerFunc(func(_ context.Context, request privileged.IssueIPCertRequest) (privileged.IssueIPCertResult, error) {
			*calls++
			*captured = request
			return privileged.IssueIPCertResult{CertPath: request.CertPath, KeyPath: request.KeyPath}, issuerErr
		})
	}
	t.Cleanup(func() { certRenewFence, certRenewIssuer = oldFence, oldIssuer })
	return captured, calls
}

type certRenewIssuerFunc func(context.Context, privileged.IssueIPCertRequest) (privileged.IssueIPCertResult, error)

func (f certRenewIssuerFunc) IssueIPCert(ctx context.Context, request privileged.IssueIPCertRequest) (privileged.IssueIPCertResult, error) {
	return f(ctx, request)
}

func TestCertRenewRejectsInvalidPublicIP(t *testing.T) {
	if err := runCertRenew(certTestCommand(&bytes.Buffer{}), "not-an-ip", "", 80); err == nil {
		t.Fatal("invalid --public-ip must fail before touching the helper")
	}
}

func TestCertRenewIssuesViaPrivilegedHelper(t *testing.T) {
	etc := t.TempDir()
	t.Setenv("VEIL_ETC_DIR", etc)
	captured, calls := stubCertRenew(t, nil)

	out := &bytes.Buffer{}
	if err := runCertRenew(certTestCommand(out), "203.0.113.9", "ops@example.com", 80); err != nil {
		t.Fatalf("renew failed: %v", err)
	}
	if *calls != 1 {
		t.Fatalf("issuer invoked %d times", *calls)
	}
	if captured.PublicIPv4 != "203.0.113.9" || captured.PublicIPv6 != "" {
		t.Fatalf("literal IPv4 must land in PublicIPv4 only: %+v", captured)
	}
	if captured.Fence.Owner != "test" || captured.Fence.Generation != 7 {
		t.Fatalf("fence token not propagated: %+v", captured.Fence)
	}
	if !captured.DeferPanelRestart {
		t.Fatal("CLI renewal must defer the panel restart")
	}
	if captured.Email != "ops@example.com" {
		t.Fatalf("email flag not propagated: %q", captured.Email)
	}
	if want := filepath.Join(etc, "panel", "tls.crt"); captured.CertPath != want {
		t.Fatalf("cert path = %q, want %q", captured.CertPath, want)
	}
	if !strings.Contains(out.String(), "Issued certificate:") {
		t.Fatalf("success output missing issue confirmation:\n%s", out.String())
	}
}

func TestCertRenewIPv6LiteralStaysIPv6(t *testing.T) {
	t.Setenv("VEIL_ETC_DIR", t.TempDir())
	captured, _ := stubCertRenew(t, nil)
	if err := runCertRenew(certTestCommand(&bytes.Buffer{}), "2001:db8::7", "", 80); err != nil {
		t.Fatalf("renew failed: %v", err)
	}
	if captured.PublicIPv6 != "2001:db8::7" || captured.PublicIPv4 != "" {
		t.Fatalf("IPv6 literal must land in PublicIPv6 only: %+v", captured)
	}
}

func TestCertRenewPropagatesIssuerError(t *testing.T) {
	t.Setenv("VEIL_ETC_DIR", t.TempDir())
	stubCertRenew(t, errors.New("helper refused"))
	if err := runCertRenew(certTestCommand(&bytes.Buffer{}), "203.0.113.9", "", 80); err == nil ||
		!strings.Contains(err.Error(), "helper refused") {
		t.Fatalf("issuer error must propagate, got %v", err)
	}
}

func TestCertRenewPropagatesFenceError(t *testing.T) {
	t.Setenv("VEIL_ETC_DIR", t.TempDir())
	oldFence, oldIssuer := certRenewFence, certRenewIssuer
	certRenewFence = func() (privileged.FenceToken, func(), error) {
		return privileged.FenceToken{}, nil, errors.New("another operation holds the runtime fencing lease; retry after it finishes")
	}
	issuerCalled := false
	certRenewIssuer = func(string) privileged.IPCertIssuer {
		return certRenewIssuerFunc(func(context.Context, privileged.IssueIPCertRequest) (privileged.IssueIPCertResult, error) {
			issuerCalled = true
			return privileged.IssueIPCertResult{}, nil
		})
	}
	t.Cleanup(func() { certRenewFence, certRenewIssuer = oldFence, oldIssuer })

	err := runCertRenew(certTestCommand(&bytes.Buffer{}), "203.0.113.9", "", 80)
	if err == nil || !strings.Contains(err.Error(), "fencing lease") {
		t.Fatalf("fence conflict must propagate, got %v", err)
	}
	if issuerCalled {
		t.Fatal("issuer ran despite a fencing failure")
	}
}
