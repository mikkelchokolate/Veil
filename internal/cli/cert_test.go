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
	if err := runCertStatus(certTestCommand(out), false, ""); err != nil {
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
	if err := runCertStatus(certTestCommand(out), false, ""); err != nil {
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
	if err := runCertStatus(certTestCommand(out), false, ""); err != nil {
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
	if err := runCertStatus(certTestCommand(out), true, ""); err != nil {
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
	certRenewFence = func(string) (privileged.FenceToken, func(), error) {
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
	if err := runCertRenew(certTestCommand(&bytes.Buffer{}), "not-an-ip", "", 80, "", ""); err == nil {
		t.Fatal("invalid --public-ip must fail before touching the helper")
	}
}

func TestCertRenewIssuesViaPrivilegedHelper(t *testing.T) {
	etc := t.TempDir()
	t.Setenv("VEIL_ETC_DIR", etc)
	captured, calls := stubCertRenew(t, nil)

	out := &bytes.Buffer{}
	if err := runCertRenew(certTestCommand(out), "203.0.113.9", "ops@example.com", 80, "", ""); err != nil {
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
	if err := runCertRenew(certTestCommand(&bytes.Buffer{}), "2001:db8::7", "", 80, "", ""); err != nil {
		t.Fatalf("renew failed: %v", err)
	}
	if captured.PublicIPv6 != "2001:db8::7" || captured.PublicIPv4 != "" {
		t.Fatalf("IPv6 literal must land in PublicIPv6 only: %+v", captured)
	}
}

func TestCertRenewPropagatesIssuerError(t *testing.T) {
	t.Setenv("VEIL_ETC_DIR", t.TempDir())
	stubCertRenew(t, errors.New("helper refused"))
	if err := runCertRenew(certTestCommand(&bytes.Buffer{}), "203.0.113.9", "", 80, "", ""); err == nil ||
		!strings.Contains(err.Error(), "helper refused") {
		t.Fatalf("issuer error must propagate, got %v", err)
	}
}

func TestCertRenewPropagatesFenceError(t *testing.T) {
	t.Setenv("VEIL_ETC_DIR", t.TempDir())
	oldFence, oldIssuer := certRenewFence, certRenewIssuer
	certRenewFence = func(string) (privileged.FenceToken, func(), error) {
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

	err := runCertRenew(certTestCommand(&bytes.Buffer{}), "203.0.113.9", "", 80, "", "")
	if err == nil || !strings.Contains(err.Error(), "fencing lease") {
		t.Fatalf("fence conflict must propagate, got %v", err)
	}
	if issuerCalled {
		t.Fatal("issuer ran despite a fencing failure")
	}
}

// certFlagCommand returns a command with the cert renew flag set so tests
// can exercise the explicit-vs-persisted precedence (Changed tracking).
func certFlagCommand(out *bytes.Buffer) *cobra.Command {
	cmd := certTestCommand(out)
	cmd.Flags().String("public-ip", "auto", "")
	cmd.Flags().Int("port", 80, "")
	cmd.Flags().String("email", "", "")
	return cmd
}

func writeVeilEnv(t *testing.T, etcDir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(etcDir, "veil.env"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Issue #1186: without an explicit --public-ip, renewal reuses the
// install-time persisted identity instead of probing detection endpoints.
func TestCertRenewUsesPersistedPublicIP(t *testing.T) {
	etc := t.TempDir()
	t.Setenv("VEIL_ETC_DIR", etc)
	writeVeilEnv(t, etc, "VEIL_PANEL_PUBLIC_IP=203.0.113.9,2001:db8::7\n")
	captured, calls := stubCertRenew(t, nil)

	cmd := certFlagCommand(&bytes.Buffer{})
	if err := runCertRenew(cmd, "auto", "", 80, "", ""); err != nil {
		t.Fatalf("renew failed: %v", err)
	}
	if *calls != 1 {
		t.Fatalf("issuer invoked %d times", *calls)
	}
	if captured.PublicIPv4 != "203.0.113.9" || captured.PublicIPv6 != "2001:db8::7" {
		t.Fatalf("persisted dual-stack identity not reused: %+v", captured)
	}
}

// An explicit --public-ip always wins over the persisted value.
func TestCertRenewExplicitPublicIPBeatsPersisted(t *testing.T) {
	etc := t.TempDir()
	t.Setenv("VEIL_ETC_DIR", etc)
	writeVeilEnv(t, etc, "VEIL_PANEL_PUBLIC_IP=203.0.113.9\n")
	captured, _ := stubCertRenew(t, nil)

	cmd := certFlagCommand(&bytes.Buffer{})
	if err := cmd.Flags().Set("public-ip", "192.0.2.55"); err != nil {
		t.Fatal(err)
	}
	if err := runCertRenew(cmd, "192.0.2.55", "", 80, "", ""); err != nil {
		t.Fatalf("renew failed: %v", err)
	}
	if captured.PublicIPv4 != "192.0.2.55" {
		t.Fatalf("explicit --public-ip lost to the persisted value: %+v", captured)
	}
}

// The persisted --le-ip-cert-port wins over the flag default but an
// explicit --port still overrides it.
func TestCertRenewUsesPersistedHTTP01Port(t *testing.T) {
	etc := t.TempDir()
	t.Setenv("VEIL_ETC_DIR", etc)
	writeVeilEnv(t, etc, "VEIL_PANEL_PUBLIC_IP=203.0.113.9\nVEIL_PANEL_HTTP01_PORT=8080\n")
	captured, _ := stubCertRenew(t, nil)

	if err := runCertRenew(certFlagCommand(&bytes.Buffer{}), "auto", "", 80, "", ""); err != nil {
		t.Fatalf("renew failed: %v", err)
	}
	if captured.HTTPPort != 8080 {
		t.Fatalf("persisted HTTP-01 port not used: %+v", captured)
	}

	captured2, _ := stubCertRenew(t, nil)
	cmd := certFlagCommand(&bytes.Buffer{})
	if err := cmd.Flags().Set("port", "8443"); err != nil {
		t.Fatal(err)
	}
	if err := runCertRenew(cmd, "auto", "", 8443, "", ""); err != nil {
		t.Fatalf("renew failed: %v", err)
	}
	if captured2.HTTPPort != 8443 {
		t.Fatalf("explicit --port lost to the persisted value: %+v", captured2)
	}
}

// Issue #1189: the controlled-CA knobs persisted in veil.env reach the
// privileged issuance request even though the CLI runs outside
// veil.service's EnvironmentFile.
func TestCertRenewPropagatesPersistedCAConfig(t *testing.T) {
	etc := t.TempDir()
	t.Setenv("VEIL_ETC_DIR", etc)
	writeVeilEnv(t, etc,
		"VEIL_PANEL_PUBLIC_IP=203.0.113.9\n"+
			"VEIL_ACME_CA_URL=https://127.0.0.1:14000/dir\n"+
			"VEIL_ACME_INSECURE=1\n"+
			"VEIL_ACME_CA_ROOT=/etc/veil/acme-root.pem\n")
	captured, _ := stubCertRenew(t, nil)

	if err := runCertRenew(certFlagCommand(&bytes.Buffer{}), "auto", "", 80, "", ""); err != nil {
		t.Fatalf("renew failed: %v", err)
	}
	if captured.CAServer != "https://127.0.0.1:14000/dir" {
		t.Fatalf("persisted CA URL not propagated: %q", captured.CAServer)
	}
	if !captured.Insecure {
		t.Fatal("persisted VEIL_ACME_INSECURE did not reach the request")
	}
	if captured.CARoot != "/etc/veil/acme-root.pem" {
		t.Fatalf("persisted CA root not propagated: %q", captured.CARoot)
	}
}

// A process-environment override still beats the persisted file value.
func TestCertRenewEnvOverridesPersistedCA(t *testing.T) {
	etc := t.TempDir()
	t.Setenv("VEIL_ETC_DIR", etc)
	t.Setenv("VEIL_ACME_CA_URL", "https://shell.example/dir")
	writeVeilEnv(t, etc,
		"VEIL_PANEL_PUBLIC_IP=203.0.113.9\nVEIL_ACME_CA_URL=https://127.0.0.1:14000/dir\n")
	captured, _ := stubCertRenew(t, nil)

	if err := runCertRenew(certFlagCommand(&bytes.Buffer{}), "auto", "", 80, "", ""); err != nil {
		t.Fatalf("renew failed: %v", err)
	}
	if captured.CAServer != "https://shell.example/dir" {
		t.Fatalf("process env must override veil.env, got %q", captured.CAServer)
	}
}

// --etc-dir retargets both the veil.env lookup and the default cert paths.
func TestCertRenewHonorsEtcDirFlag(t *testing.T) {
	etc := t.TempDir()
	writeVeilEnv(t, etc, "VEIL_PANEL_PUBLIC_IP=203.0.113.9\n")
	captured, _ := stubCertRenew(t, nil)

	if err := runCertRenew(certFlagCommand(&bytes.Buffer{}), "auto", "", 80, etc, ""); err != nil {
		t.Fatalf("renew failed: %v", err)
	}
	want := filepath.Join(etc, "panel", "tls.crt")
	if captured.CertPath != want {
		t.Fatalf("cert path = %q, want %q", captured.CertPath, want)
	}
}

// Issue #1187: a persisted --le-ip-cert=false must stop `veil cert renew`
// too — issuing once while the daemon keeps skipping renewals would just
// leave the panel on an expiring certificate. An explicit env override
// (VEIL_PANEL_LE_IP_CERT=1) still forces a renewal.
func TestCertRenewHonorsPersistedOptOut(t *testing.T) {
	etc := t.TempDir()
	t.Setenv("VEIL_ETC_DIR", etc)
	writeVeilEnv(t, etc, "VEIL_PANEL_PUBLIC_IP=203.0.113.9\nVEIL_PANEL_LE_IP_CERT=0\n")
	_, calls := stubCertRenew(t, nil)

	out := &bytes.Buffer{}
	if err := runCertRenew(certFlagCommand(out), "auto", "", 80, "", ""); err != nil {
		t.Fatalf("opted-out renew should be a clean no-op, got %v", err)
	}
	if *calls != 0 {
		t.Fatal("issuer ran despite the persisted opt-out")
	}
	if !strings.Contains(out.String(), "disabled") {
		t.Fatalf("opt-out notice missing from output:\n%s", out.String())
	}

	// Escape hatch: an explicit process-env override beats the file.
	t.Setenv("VEIL_PANEL_LE_IP_CERT", "1")
	captured, calls2 := stubCertRenew(t, nil)
	if err := runCertRenew(certFlagCommand(&bytes.Buffer{}), "auto", "", 80, "", ""); err != nil {
		t.Fatalf("env-forced renew failed: %v", err)
	}
	if *calls2 != 1 || captured.PublicIPv4 != "203.0.113.9" {
		t.Fatalf("explicit VEIL_PANEL_LE_IP_CERT=1 must re-enable renewal: %+v", captured)
	}
}
