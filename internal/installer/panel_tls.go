package installer

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"time"
)

type PanelTLS struct{}

type PanelTLSMaterial struct {
	CertPEM string
	KeyPEM  string
}

func NewPanelTLS() PanelTLS { return PanelTLS{} }

func (PanelTLS) Generate(domain string) (PanelTLSMaterial, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return PanelTLSMaterial{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return PanelTLSMaterial{}, err
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
		return PanelTLSMaterial{}, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return PanelTLSMaterial{CertPEM: string(certPEM), KeyPEM: string(keyPEM)}, nil
}
