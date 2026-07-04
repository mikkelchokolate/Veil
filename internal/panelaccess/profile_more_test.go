package panelaccess

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"strings"
	"testing"
)

func TestRecommendedListenDefaultsPort(t *testing.T) {
	if got := RecommendedListen("local", 0); got != "127.0.0.1:2096" {
		t.Fatalf("RecommendedListen(local, 0) = %q", got)
	}
	if got := RecommendedListen("direct", -1); got != "0.0.0.0:2096" {
		t.Fatalf("RecommendedListen(direct, -1) = %q", got)
	}
}

func TestNewWebBasePathPolicyUsesDefaultReaderWhenNil(t *testing.T) {
	policy := NewWebBasePathPolicy(nil)
	path := policy.Generate()
	if path == "" || !strings.HasPrefix(path, "/") || !strings.HasSuffix(path, "/") {
		t.Fatalf("unexpected path %q", path)
	}
}

func TestProfileBuildRejectsInvalidDomain(t *testing.T) {
	_, err := NewProfile(ProfileInput{PanelAccess: "caddy", Domain: "not-a-domain", Email: "admin@example.com", PanelPort: 2096}).Build()
	if err == nil || !strings.Contains(err.Error(), "domain") {
		t.Fatalf("expected domain validation error, got %v", err)
	}
}

func TestProfileBuildRejectsInvalidEmail(t *testing.T) {
	_, err := NewProfile(ProfileInput{PanelAccess: "caddy", Domain: "panel.example.com", Email: "not-an-email", PanelPort: 2096}).Build()
	if err == nil || !strings.Contains(err.Error(), "email") {
		t.Fatalf("expected email validation error, got %v", err)
	}
}

func TestProfileBuildPropagatesCaddyfileRenderError(t *testing.T) {
	// Valid domain/email but invalid panel port triggers renderer error after validation.
	_, err := NewProfile(ProfileInput{PanelAccess: "caddy", Domain: "panel.example.com", Email: "admin@example.com", PanelPort: 0}).Build()
	if err == nil || !strings.Contains(err.Error(), "panel port is required") {
		t.Fatalf("expected panel port render error, got %v", err)
	}
}

func TestProfileBuildPropagatesTLSGenerateError(t *testing.T) {
	original := rsaGenerateKey
	rsaGenerateKey = func(r io.Reader, bits int) (*rsa.PrivateKey, error) { return nil, errors.New("key failure") }
	defer func() { rsaGenerateKey = original }()

	_, err := NewProfile(ProfileInput{PanelAccess: "local", PanelPort: 2096}).Build()
	if err == nil || !strings.Contains(err.Error(), "key failure") {
		t.Fatalf("expected TLS generation error, got %v", err)
	}
}

func TestTLSGenerateIncludesDomainAsIPAddress(t *testing.T) {
	ip := net.ParseIP("203.0.113.10")
	material, err := NewTLS().Generate("203.0.113.10", nil)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	block, _ := pem.Decode([]byte(material.CertPEM))
	if block == nil {
		t.Fatal("failed to decode certificate PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}
	found := false
	for _, got := range cert.IPAddresses {
		if got.Equal(ip) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected IP %v in certificate, got %v", ip, cert.IPAddresses)
	}
}

func TestTLSGenerateReturnsRSAKeyError(t *testing.T) {
	original := rsaGenerateKey
	rsaGenerateKey = func(r io.Reader, bits int) (*rsa.PrivateKey, error) { return nil, errors.New("rsa boom") }
	defer func() { rsaGenerateKey = original }()

	_, err := NewTLS().Generate("panel.example.com", nil)
	if err == nil || !strings.Contains(err.Error(), "rsa boom") {
		t.Fatalf("expected RSA key error, got %v", err)
	}
}

func TestTLSGenerateReturnsSerialError(t *testing.T) {
	original := randInt
	randInt = func(r io.Reader, max *big.Int) (*big.Int, error) { return nil, errors.New("serial boom") }
	defer func() { randInt = original }()

	_, err := NewTLS().Generate("panel.example.com", nil)
	if err == nil || !strings.Contains(err.Error(), "serial boom") {
		t.Fatalf("expected serial error, got %v", err)
	}
}

func TestTLSGenerateReturnsCreateCertificateError(t *testing.T) {
	original := x509CreateCertificate
	x509CreateCertificate = func(r io.Reader, template, parent *x509.Certificate, pub interface{}, priv interface{}) ([]byte, error) {
		return nil, errors.New("cert boom")
	}
	defer func() { x509CreateCertificate = original }()

	_, err := NewTLS().Generate("panel.example.com", nil)
	if err == nil || !strings.Contains(err.Error(), "cert boom") {
		t.Fatalf("expected certificate error, got %v", err)
	}
}

func TestNonLoopbackInterfaceIPsReturnsNilOnError(t *testing.T) {
	original := interfaceAddrs
	interfaceAddrs = func() ([]net.Addr, error) { return nil, errors.New("interfaces unavailable") }
	defer func() { interfaceAddrs = original }()

	ips := nonLoopbackInterfaceIPs()
	if len(ips) != 0 {
		t.Fatalf("expected nil/empty on error, got %v", ips)
	}
}

func TestNonLoopbackInterfaceIPsHandlesIPAddr(t *testing.T) {
	original := interfaceAddrs
	interfaceAddrs = func() ([]net.Addr, error) {
		return []net.Addr{
			&net.IPAddr{IP: net.ParseIP("203.0.113.5")},
			&net.IPAddr{IP: net.ParseIP("127.0.0.1")},
		}, nil
	}
	defer func() { interfaceAddrs = original }()

	ips := nonLoopbackInterfaceIPs()
	if len(ips) != 1 || !ips[0].Equal(net.ParseIP("203.0.113.5")) {
		t.Fatalf("expected 203.0.113.5, got %v", ips)
	}
}

func TestNonLoopbackInterfaceIPsSkipsNilIP(t *testing.T) {
	original := interfaceAddrs
	interfaceAddrs = func() ([]net.Addr, error) {
		return []net.Addr{
			&net.IPNet{IP: net.ParseIP("203.0.113.7")},
			&mockAddr{},
		}, nil
	}
	defer func() { interfaceAddrs = original }()

	ips := nonLoopbackInterfaceIPs()
	if len(ips) != 1 || !ips[0].Equal(net.ParseIP("203.0.113.7")) {
		t.Fatalf("expected 203.0.113.7, got %v", ips)
	}
}

type mockAddr struct{}

func (mockAddr) Network() string { return "mock" }
func (mockAddr) String() string  { return "mock" }
