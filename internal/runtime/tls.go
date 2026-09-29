package runtime

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"strings"
	"time"
)

// Certificate source classifications reported to operators. The managed
// values (acme/internal/self-signed/missing) describe the certificate a
// managed runtime actually serves and are deliberately honest: a Veil-issued
// fallback or Caddy local-CA certificate is never reported as "trusted"
// (#1168). "env"/"caddy" remain for the panel-edge endpoint where the source
// describes WHERE the file was loaded from.
const (
	TLSCertSourceEnv        = "env"
	TLSCertSourceCaddy      = "caddy"
	TLSCertSourceACME       = "acme"
	TLSCertSourceInternal   = "internal"
	TLSCertSourceSelfSigned = "self-signed"
	TLSCertSourceMissing    = "missing"
)

// caddyLocalIssuerCNPrefix is the common-name prefix Caddy gives its local
// (internal) authority — "Caddy Local Authority - ECC Intermediate" for
// managed leaf certificates. A leaf signed by that authority is Veil/Caddy
// internal material, not a publicly trusted ACME certificate (#1168).
const caddyLocalIssuerCNPrefix = "Caddy Local Authority"

// TLSCertInfo holds TLS certificate information.
type TLSCertInfo struct {
	Path string `json:"path"`
	// Source classifies where the certificate came from. Panel-edge reads use
	// "env" (VEIL_TLS_CERT) or "caddy" (Caddy-managed ACME storage). Per-inbound
	// reads use TLSCertSourceACME/Internal/SelfSigned/Missing so a Veil-issued
	// fallback never masquerades as a trusted ACME issuance (#1168).
	Source        string   `json:"source,omitempty"`
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
	info, _ := readTLSCertParsed(path, strings.TrimSpace(domain))
	return info
}

// ReadManagedTLSCertForDomain reads the certificate a managed runtime
// (e.g. a hysteria2 inbound) is actually configured to serve and classifies
// its origin honestly: "acme" for a CA-issued certificate, "internal" for a
// Caddy local-CA certificate, "self-signed" for a self-signed/fallback
// certificate, and "missing" when the file is absent or unreadable (#1168).
// The SAN check against domain still applies when domain is non-empty.
func ReadManagedTLSCertForDomain(path, domain string) TLSCertInfo {
	info, cert := readTLSCertParsed(path, strings.TrimSpace(domain))
	if cert == nil {
		// No parsable certificate — the runtime has nothing (or garbage) at
		// the configured path. Keep the read error detail and mark the source
		// missing rather than guessing at an issuer.
		info.Source = TLSCertSourceMissing
		return info
	}
	info.Source = ClassifyManagedCertSource(cert)
	return info
}

// ClassifyManagedCertSource reports the origin of a parsed leaf certificate
// served by a managed runtime. Self-signed certificates (subject == issuer
// and the signature verifies under the certificate's own public key) are
// "self-signed"; certificates issued by Caddy's local authority are
// "internal"; anything else is assumed CA-issued and reported "acme". The
// classification never upgrades a fallback to "acme" — an unrecognized
// issuer is still reported acme only when the certificate is NOT
// self-signed/internal (#1168).
func ClassifyManagedCertSource(cert *x509.Certificate) string {
	if cert == nil {
		return TLSCertSourceMissing
	}
	if IsSelfSignedCert(cert) {
		return TLSCertSourceSelfSigned
	}
	if strings.HasPrefix(strings.TrimSpace(cert.Issuer.CommonName), caddyLocalIssuerCNPrefix) {
		return TLSCertSourceInternal
	}
	return TLSCertSourceACME
}

// IsSelfSignedCert reports whether cert is signed by its own key: the raw
// subject equals the raw issuer and the signature verifies under the
// certificate's own public key. Unlike CheckSignatureFrom this does not
// require CA basic constraints, so it correctly identifies the Veil fallback
// leaf which is not a CA (#1168).
func IsSelfSignedCert(cert *x509.Certificate) bool {
	if cert == nil {
		return false
	}
	if !bytes.Equal(cert.RawSubject, cert.RawIssuer) {
		return false
	}
	return cert.CheckSignature(cert.SignatureAlgorithm, cert.RawTBSCertificate, cert.Signature) == nil
}

func readTLSCert(path, domain string) TLSCertInfo {
	info, _ := readTLSCertParsed(path, domain)
	return info
}

// readTLSCertParsed reads/parses the certificate file and returns both the
// wire-facing info and the parsed leaf (nil when the file is missing,
// unreadable, or unparseable).
func readTLSCertParsed(path, domain string) (TLSCertInfo, *x509.Certificate) {
	info := TLSCertInfo{Path: path, Valid: false}
	if path == "" {
		info.Error = "no certificate path configured"
		return info, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		info.Error = fmt.Sprintf("read certificate: %v", err)
		return info, nil
	}
	block, _ := pem.Decode(data)
	if block == nil {
		info.Error = "failed to decode PEM block"
		return info, nil
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		info.Error = fmt.Sprintf("parse certificate: %v", err)
		return info, nil
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
	return info, cert
}
