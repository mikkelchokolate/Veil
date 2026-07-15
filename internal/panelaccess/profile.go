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
	"strconv"
	"time"

	"github.com/mikkelchokolate/Veil/internal/caddyassembly"
	"github.com/mikkelchokolate/Veil/internal/caddycapabilities"
	"github.com/mikkelchokolate/Veil/internal/hostenv"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/renderer"
)

type ProfileInput struct {
	PanelAccess string
	Domain      string
	Email       string
	PanelDomain string
	PanelEmail  string
	PanelPort   int
	PublicPort  int
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
	PanelPublicPort   int
	PanelTLSEnabled   bool
	PanelTLSCertPEM   string
	PanelTLSKeyPEM    string
	WebBasePath       string
	InstallPanelCaddy bool
	CaddyJSON         string
}

type Profile struct {
	settings   model.Settings
	PublicPort int
}

func NewProfile(input ProfileInput) Profile {
	publicPort := input.PublicPort
	if publicPort == 0 {
		publicPort = 443
	}
	settings := model.Settings{
		PanelAccess:     input.PanelAccess,
		PanelDomain:     input.PanelDomain,
		Domain:          input.Domain,
		PanelEmail:      input.PanelEmail,
		Email:           input.Email,
		PanelListen:     RecommendedListen(input.PanelAccess, input.PanelPort),
		PanelPublicPort: publicPort,
	}
	return Profile{settings: settings, PublicPort: publicPort}
}

// BuildProfile builds a Panel access Profile from model settings. It defaults
// PanelPublicPort to 443 when zero and resolves the panel-specific domain/email
// with fallback to the legacy Domain/Email fields.
func BuildProfile(settings model.Settings) (Profile, error) {
	publicPort := settings.PanelPublicPort
	if publicPort == 0 {
		publicPort = 443
	}
	if _, err := panelPortFromListen(settings.PanelListen); err != nil {
		return Profile{}, err
	}
	settings.PanelPublicPort = publicPort
	return Profile{settings: settings, PublicPort: publicPort}, nil
}

func panelPortFromListen(listen string) (int, error) {
	if listen == "" {
		return 0, fmt.Errorf("panelListen is required")
	}
	_, portStr, err := net.SplitHostPort(listen)
	if err != nil {
		return 0, fmt.Errorf("panelListen must be host:port")
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("panelListen port must be a valid integer between 1 and 65535")
	}
	return port, nil
}

// newTLSFunc is overridable in tests so that Profile.Build TLS error paths can
// be exercised without mocking the crypto/rand package.
var newTLSFunc = NewTLS

func (p Profile) Build() (ProfileMaterial, error) {
	settings := p.settings
	material := ProfileMaterial{PanelListen: settings.PanelListen, PanelPublicPort: p.PublicPort}
	material.WebBasePath = NewWebBasePathPolicy(rand.Reader).Generate()
	settings.WebBasePath = material.WebBasePath
	if settings.PanelDomain == "" {
		settings.PanelDomain = settings.Domain
	}
	if settings.PanelEmail == "" {
		settings.PanelEmail = settings.Email
	}
	panelCaddy := settings.PanelAccess == "caddy"
	if panelCaddy {
		if err := hostenv.ValidateDomain(settings.PanelDomain); err != nil {
			return ProfileMaterial{}, err
		}
		if err := hostenv.ValidateEmail(settings.PanelEmail); err != nil {
			return ProfileMaterial{}, err
		}
		material.InstallPanelCaddy = true
		plan, _, err := caddyassembly.BuildRenderPlan(settings, nil, nil)
		if err != nil {
			return ProfileMaterial{}, err
		}
		body, err := renderer.RenderCaddyJSON(plan, caddycapabilities.CaddyCapabilities{})
		if err != nil {
			return ProfileMaterial{}, err
		}
		material.CaddyJSON = string(body)
		return material, nil
	}
	var extraIPs []net.IP
	if settings.PanelAccess == "direct" {
		extraIPs = nonLoopbackInterfaceIPs()
	}
	panelTLS, err := newTLSFunc().Generate(settings.PanelDomain, extraIPs)
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

// Crypto operation hooks are overridable in tests so the error branches inside
// TLS.Generate can be exercised without depending on rand.Reader failures.
var (
	rsaGenerateKey        = rsa.GenerateKey
	randInt               = rand.Int
	x509CreateCertificate = x509.CreateCertificate
)

func (TLS) Generate(domain string, extraIPs []net.IP) (TLSMaterial, error) {
	key, err := rsaGenerateKey(rand.Reader, 2048)
	if err != nil {
		return TLSMaterial{}, err
	}
	serial, err := randInt(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
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
	cert.IPAddresses = append(cert.IPAddresses, extraIPs...)
	if domain != "" {
		if ip := net.ParseIP(domain); ip != nil {
			cert.IPAddresses = append(cert.IPAddresses, ip)
		} else {
			cert.DNSNames = append(cert.DNSNames, domain)
		}
	}
	der, err := x509CreateCertificate(rand.Reader, &cert, &cert, &key.PublicKey, key)
	if err != nil {
		return TLSMaterial{}, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return TLSMaterial{CertPEM: string(certPEM), KeyPEM: string(keyPEM)}, nil
}

// interfaceAddrs is overridable in tests so error paths and unusual address
// types can be exercised without depending on the host's network configuration.
var interfaceAddrs = net.InterfaceAddrs

// nonLoopbackInterfaceIPs returns all non-loopback IP addresses assigned to
// local network interfaces. For direct Panel access this lets the self-signed
// certificate match the public interface IP, avoiding browser certificate
// warnings that interact badly with cached HSTS policies.
func nonLoopbackInterfaceIPs() []net.IP {
	addrs, err := interfaceAddrs()
	if err != nil {
		return nil
	}
	var ips []net.IP
	for _, addr := range addrs {
		var ip net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if ip == nil || ip.IsLoopback() {
			continue
		}
		ips = append(ips, ip)
	}
	return ips
}
