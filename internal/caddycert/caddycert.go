// Package caddycert locates TLS certificates managed by Caddy's ACME/Issuer
// storage so other Veil runtimes (Hysteria2, etc.) can reuse the same
// Let's Encrypt certificates instead of serving self-signed ones.
package caddycert

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultCaddyDataDir is the default XDG data home used by the veil-caddy@
// systemd units (XDG_DATA_HOME=/var/lib/caddy).
const DefaultCaddyDataDir = "/var/lib/caddy"

// defaultDataDir mirrors DefaultCaddyDataDir but is overridable by tests so
// the empty-data-dir branch can be exercised without relying on /var/lib/caddy.
var defaultDataDir = DefaultCaddyDataDir

// Pair holds the filesystem paths to a matched certificate and private key.
// IssuerName is the Caddy issuer storage directory the pair was found under
// (e.g. "local" for Caddy's internal CA or "acme-v02.api.letsencrypt.org-
// directory" for an ACME issuer), so callers can tell a silently degraded
// internal fallback apart from a publicly trusted ACME issuance (#906).
type Pair struct {
	CertPath   string
	KeyPath    string
	IssuerName string
}

// IssuerKind classifies a Caddy issuer storage directory name.
// "internal" is Caddy's local CA — browsers do not trust it, so a panel or
// inbound silently falling back to it is a degraded state that must stay
// visible (#906).
func IssuerKind(issuerName string) string {
	switch {
	case issuerName == "local":
		return "internal"
	case strings.HasPrefix(issuerName, "acme"):
		return "acme"
	case issuerName == "":
		return ""
	default:
		return "other"
	}
}

// FindPair searches Caddy certificate storage for a valid, non-expired
// certificate for domain. It prefers ACME-issued certificates over Caddy's
// local/internal CA. If no usable certificate is found, it returns
// ErrCertificateNotFound.
func FindPair(caddyDataDir, domain string) (Pair, error) {
	if caddyDataDir == "" {
		caddyDataDir = defaultDataDir
	}
	if domain == "" {
		return Pair{}, errors.New("domain is required")
	}

	certsRoot := filepath.Join(caddyDataDir, "caddy", "certificates")
	entries, err := os.ReadDir(certsRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Pair{}, ErrCertificateNotFound
		}
		return Pair{}, fmt.Errorf("read Caddy certificates root: %w", err)
	}

	var best *Pair
	bestScore := -1
	bestModTime := time.Time{}

	for _, issuerEntry := range entries {
		if !issuerEntry.IsDir() {
			continue
		}
		issuerName := issuerEntry.Name()
		domainDir := filepath.Join(certsRoot, issuerName, domain)
		certPath := filepath.Join(domainDir, domain+".crt")
		keyPath := filepath.Join(domainDir, domain+".key")

		certInfo, err := os.Stat(certPath)
		if err != nil {
			continue
		}
		if _, err := os.Stat(keyPath); err != nil {
			continue
		}
		if !isValidCertificate(certPath, domain) {
			continue
		}

		score := issuerScore(issuerName)
		modTime := certInfo.ModTime()
		if score > bestScore || (score == bestScore && modTime.After(bestModTime)) {
			best = &Pair{CertPath: certPath, KeyPath: keyPath, IssuerName: issuerName}
			bestScore = score
			bestModTime = modTime
		}
	}

	if best == nil {
		return Pair{}, ErrCertificateNotFound
	}
	return *best, nil
}

// ErrCertificateNotFound is returned when no usable Caddy-managed certificate
// exists for the requested domain.
var ErrCertificateNotFound = errors.New("no usable Caddy-managed certificate found")

// issuerScore returns a preference score for a Caddy issuer directory.
// ACME issuers (Let's Encrypt, ZeroSSL, etc.) rank higher than Caddy's
// internal/local CA.
func issuerScore(issuerName string) int {
	if issuerName == "local" {
		return 0
	}
	if strings.HasPrefix(issuerName, "acme-") {
		return 2
	}
	return 1
}

// isValidCertificate verifies that certPath is a readable, non-expired
// certificate whose SANs include domain.
func isValidCertificate(certPath, domain string) bool {
	data, err := os.ReadFile(certPath)
	if err != nil {
		return false
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return false
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false
	}
	now := time.Now()
	if now.Before(cert.NotBefore) || now.After(cert.NotAfter) {
		return false
	}
	return cert.VerifyHostname(domain) == nil
}
