package acmeip

// Regression coverage for issue #665: each IssueOptions address field must
// carry only addresses of its own family, and IPv6-only issuance must work
// for hosts without public IPv4.

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestIssueIPCertRejectsIPv6InIPv4Field(t *testing.T) {
	_, err := IssueIPCert(context.Background(), IssueOptions{PublicIPv4: "2001:db8::1", System: newFakeSystem()})
	if err == nil || !strings.Contains(err.Error(), "IPv4") {
		t.Fatalf("expected IPv4-family rejection, got %v", err)
	}
}

func TestIssueIPCertRejectsIPv4InIPv6Field(t *testing.T) {
	_, err := IssueIPCert(context.Background(), IssueOptions{PublicIPv4: "1.2.3.4", PublicIPv6: "5.6.7.8", System: newFakeSystem()})
	if err == nil || !strings.Contains(err.Error(), "IPv6") {
		t.Fatalf("expected IPv6-family rejection, got %v", err)
	}
}

func TestIssueIPCertRejectsIPv4MappedIPv6InIPv6Field(t *testing.T) {
	// "::ffff:1.2.3.4" parses as IPv6-shaped but is really IPv4 — it must not
	// satisfy the IPv6 SAN slot.
	_, err := IssueIPCert(context.Background(), IssueOptions{PublicIPv4: "1.2.3.4", PublicIPv6: "::ffff:1.2.3.4", System: newFakeSystem()})
	if err == nil || !strings.Contains(err.Error(), "IPv6") {
		t.Fatalf("expected IPv6-family rejection, got %v", err)
	}
}

func TestIssueIPCertRequiresSomePublicIP(t *testing.T) {
	_, err := IssueIPCert(context.Background(), IssueOptions{System: newFakeSystem()})
	if err == nil {
		t.Fatal("expected error when no public IP is supplied")
	}
}

func TestIssueIPCertIssuesIPv6OnlyCertificate(t *testing.T) {
	sys := newFakeSystem()
	sys.setAcmeInstalled()
	sys.lookPaths["socat"] = "/usr/bin/socat"
	sys.installIPs = []string{"2001:db8::1"}

	acmeSh := filepath.Join(sys.home, ".acme.sh", "acme.sh")
	sys.commands[sys.key(acmeSh, "--set-default-ca", "--server", "letsencrypt")] = commandResult{out: "OK"}
	// IPv6-only issuance uses the IPv6 identity as the primary -d.
	sys.commands[sys.key(acmeSh, "--issue", "-d", "2001:db8::1", "--standalone", "--server", "letsencrypt", "--certificate-profile", "shortlived", "--days", "3", "--httpport", "80", "--force")] = commandResult{out: "Cert issued"}
	sys.commands[sys.key(acmeSh, "--installcert", "-d", "2001:db8::1", "--key-file", "/etc/veil/panel/tls.key", "--fullchain-file", "/etc/veil/panel/tls.crt", "--reloadcmd", renewReloadCmd("/etc/veil/panel/tls.crt", "/etc/veil/panel/tls.key"))] = commandResult{out: "Installed"}

	cert, err := IssueIPCert(context.Background(), IssueOptions{PublicIPv6: "2001:db8::1", System: sys})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cert.CertPath != "/etc/veil/panel/tls.crt" {
		t.Fatalf("unexpected cert path: %+v", cert)
	}
}
