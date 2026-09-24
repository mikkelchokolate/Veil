package runtime

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"strings"
	"time"
)

// TLSCertInfo holds TLS certificate information.
type TLSCertInfo struct {
	Path          string   `json:"path"`
	Source        string   `json:"source,omitempty"` // "env" for VEIL_TLS_CERT, "caddy" for Caddy-managed storage
	Subject       string   `json:"subject"`
	Issuer        string   `json:"issuer"`
	NotBefore     string   `json:"notBefore"`
	NotAfter      string   `json:"notAfter"`
	DaysRemaining int      `json:"daysRemaining"`
	DNSNames      []string `json:"dnsNames,omitempty"`
	Valid         bool     `json:"valid"`
	Error         string   `json:"error,omitempty"`
	// ManagedBy names the component that issued/stores the certificate when it
	// is not the process's own VEIL_TLS_CERT file (e.g. "caddy" for the managed
	// panel edge). IssuerSource is the upstream issuer identity (Caddy issuer
	// storage name) and IssuerKind classifies it ("acme" vs "internal") so an
	// ACME failure that silently fell back to Caddy's local CA stays visible
	// in status instead of looking like a trusted cert (#906).
	ManagedBy    string `json:"managedBy,omitempty"`
	IssuerSource string `json:"issuerSource,omitempty"`
	IssuerKind   string `json:"issuerKind,omitempty"`
}

// ReadTLSCert reads and parses a TLS certificate file.
func ReadTLSCert(path string) TLSCertInfo {
	return readTLSCert(path, "")
}

// ReadTLSCertForDomain reads and parses a TLS certificate file and reports it
// valid only when it also covers domain — an unexpired certificate whose SANs
// no longer match the served hostname is reported invalid so domain/SAN drift
// is visible to operators (#905). An empty domain skips the hostname check.
func ReadTLSCertForDomain(path, domain string) TLSCertInfo {
	return readTLSCert(path, strings.TrimSpace(domain))
}

func readTLSCert(path, domain string) TLSCertInfo {
	info := TLSCertInfo{Path: path, Valid: false}
	if path == "" {
		info.Error = "no certificate path configured"
		return info
	}
	data, err := os.ReadFile(path)
	if err != nil {
		info.Error = fmt.Sprintf("read certificate: %v", err)
		return info
	}
	block, _ := pem.Decode(data)
	if block == nil {
		info.Error = "failed to decode PEM block"
		return info
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		info.Error = fmt.Sprintf("parse certificate: %v", err)
		return info
	}
	now := time.Now()
	info.Subject = cert.Subject.String()
	info.Issuer = cert.Issuer.String()
	info.NotBefore = cert.NotBefore.Format(time.RFC3339)
	info.NotAfter = cert.NotAfter.Format(time.RFC3339)
	info.DaysRemaining = int(cert.NotAfter.Sub(now).Hours() / 24)
	info.DNSNames = cert.DNSNames
	info.Valid = now.Before(cert.NotAfter) && now.After(cert.NotBefore)
	if info.Valid && domain != "" {
		if err := cert.VerifyHostname(domain); err != nil {
			info.Valid = false
			info.Error = fmt.Sprintf("certificate does not cover %s: %v", domain, err)
		}
	}
	return info
}
