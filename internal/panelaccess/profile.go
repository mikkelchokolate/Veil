package panelaccess

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"time"

	"github.com/mikkelchokolate/Veil/internal/hostenv"
	"github.com/mikkelchokolate/Veil/internal/renderer"
)

type ProfileInput struct {
	PanelAccess string
	Domain      string
	Email       string
	PanelPort   int
}

type ModeResolution struct {
	Mode          string
	PanelListen   string
	RequiresCaddy bool
}

type Mode struct {
	mode string
}

func NewMode(mode string) Mode {
	return Mode{mode: mode}
}

func (m Mode) Resolve(port int) (ModeResolution, error) {
	mode := m.mode
	if mode == "" {
		mode = "local"
	}
	switch mode {
	case "direct":
		return ModeResolution{Mode: mode, PanelListen: RecommendedListen(mode, port)}, nil
	case "local":
		return ModeResolution{Mode: mode, PanelListen: RecommendedListen(mode, port)}, nil
	case "caddy":
		return ModeResolution{Mode: mode, PanelListen: RecommendedListen(mode, port), RequiresCaddy: true}, nil
	default:
		return ModeResolution{}, fmt.Errorf("panel access must be direct, local, or caddy")
	}
}

type ProfileMaterial struct {
	PanelListen       string
	PanelTLSEnabled   bool
	PanelTLSCertPEM   string
	PanelTLSKeyPEM    string
	WebBasePath       string
	InstallPanelCaddy bool
	Caddyfile         string
}

type Profile struct {
	input ProfileInput
}

func NewProfile(input ProfileInput) Profile {
	return Profile{input: input}
}

func (p Profile) Build() (ProfileMaterial, error) {
	input := p.input
	material := ProfileMaterial{PanelListen: RecommendedListen(input.PanelAccess, input.PanelPort)}
	material.WebBasePath = NewWebBasePathPolicy(rand.Reader).Generate()
	panelCaddy := input.PanelAccess == "caddy"
	if panelCaddy {
		if err := hostenv.ValidateDomain(input.Domain); err != nil {
			return ProfileMaterial{}, err
		}
		if err := hostenv.ValidateEmail(input.Email); err != nil {
			return ProfileMaterial{}, err
		}
		material.InstallPanelCaddy = true
		caddyfile, err := renderer.RenderPanelCaddyfile(renderer.PanelCaddyConfig{Domain: input.Domain, Email: input.Email, PanelPort: input.PanelPort, WebBasePath: material.WebBasePath})
		if err != nil {
			return ProfileMaterial{}, err
		}
		material.Caddyfile = caddyfile
		return material, nil
	}
	panelTLS, err := NewTLS().Generate(input.Domain)
	if err != nil {
		return ProfileMaterial{}, err
	}
	material.PanelTLSEnabled = true
	material.PanelTLSCertPEM = panelTLS.CertPEM
	material.PanelTLSKeyPEM = panelTLS.KeyPEM
	return material, nil
}

func RecommendedListen(panelAccess string, panelPort int) string {
	if panelPort <= 0 {
		panelPort = 2096
	}
	if panelAccess == "direct" {
		return fmt.Sprintf("0.0.0.0:%d", panelPort)
	}
	return fmt.Sprintf("127.0.0.1:%d", panelPort)
}

type WebBasePathPolicy struct {
	random io.Reader
}

func NewWebBasePathPolicy(random io.Reader) WebBasePathPolicy {
	if random == nil {
		random = rand.Reader
	}
	return WebBasePathPolicy{random: random}
}

func (p WebBasePathPolicy) Generate() string {
	buf := make([]byte, 9)
	if _, err := io.ReadFull(p.random, buf); err != nil {
		return "/veil-panel/"
	}
	return "/" + base64.RawURLEncoding.EncodeToString(buf) + "/"
}

type TLS struct{}

type TLSMaterial struct {
	CertPEM string
	KeyPEM  string
}

func NewTLS() TLS { return TLS{} }

func (TLS) Generate(domain string) (TLSMaterial, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return TLSMaterial{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return TLSMaterial{}, err
	}
	now := time.Now().UTC()
	cert := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "Veil Panel"},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.AddDate(10, 0, 0),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	if domain != "" {
		if ip := net.ParseIP(domain); ip != nil {
			cert.IPAddresses = append(cert.IPAddresses, ip)
		} else {
			cert.DNSNames = append(cert.DNSNames, domain)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, &cert, &cert, &key.PublicKey, key)
	if err != nil {
		return TLSMaterial{}, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return TLSMaterial{CertPEM: string(certPEM), KeyPEM: string(keyPEM)}, nil
}
