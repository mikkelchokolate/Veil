// Package caddycert locates TLS certificates managed by Caddy's ACME/Issuer
// storage so other Veil runtimes (Hysteria2, etc.) can reuse the same
// Let's Encrypt certificates instead of serving self-signed ones.
package caddycert

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mikkelchokolate/Veil/internal/safefs"
)

// DefaultCaddyDataDir is the default XDG data home used by the veil-caddy@
// systemd units (XDG_DATA_HOME=/var/lib/caddy).
const DefaultCaddyDataDir = "/var/lib/caddy"

// defaultDataDir mirrors DefaultCaddyDataDir but is overridable by tests so
// the empty-data-dir branch can be exercised without relying on /var/lib/caddy.
var defaultDataDir = DefaultCaddyDataDir

// maxPairMaterialBytes bounds a single .crt/.key read. Real ACME material is
// a few KiB; the cap keeps a swapped-in giant file from being slurped.
const maxPairMaterialBytes int64 = 1024 * 1024

// Pair holds a matched certificate and private key.
// IssuerName is the Caddy issuer storage directory the pair was found under
// (e.g. "local" for Caddy's internal CA or "acme-v02.api.letsencrypt.org-
// directory" for an ACME issuer), so callers can tell a silently degraded
// internal fallback apart from a publicly trusted ACME issuance (#906).
//
// CertPEM/KeyPEM are the exact bytes FindPair validated: they were read
// through descriptor-pinned opens (every directory component opened
// O_NOFOLLOW|O_DIRECTORY relative to its parent), so a symlink swapped into
// the tree — or a leaf swapped between validation and the caller's copy —
// cannot substitute different material than what was checked (#1086).
// CertPath/KeyPath remain for display/logging only.
type Pair struct {
	CertPath   string
	KeyPath    string
	IssuerName string
	CertPEM    []byte
	KeyPEM     []byte
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
// certificate for domain whose private key matches the certificate. It
// prefers ACME-issued certificates over Caddy's local/internal CA. If no
// usable certificate is found, it returns ErrCertificateNotFound.
func FindPair(caddyDataDir, domain string) (Pair, error) {
	if caddyDataDir == "" {
		caddyDataDir = defaultDataDir
	}
	if domain == "" {
		return Pair{}, errors.New("domain is required")
	}
	// domain names a single directory below the issuer root — a value with
	// separators or dot segments would escape the descriptor-pinned descent.
	if !filepath.IsLocal(domain) || strings.ContainsAny(domain, `/\`) {
		return Pair{}, fmt.Errorf("domain %q is not a valid storage name", domain)
	}

	certsRoot := filepath.Join(caddyDataDir, "caddy", "certificates")
	// The certificate tree lives under the veil-proxy-owned StateDirectory,
	// so certsRoot itself, each <issuer> dir, and each <domain> dir are
	// attacker-replaceable. Every component is opened O_NOFOLLOW|O_DIRECTORY
	// relative to the already-held parent descriptor: a symlink swapped into
	// any level fails ELOOP instead of redirecting the read, and the cert/key
	// bytes below are the same bytes that get validated and copied (#1086).
	rootDir, err := safefs.OpenDir(certsRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Pair{}, ErrCertificateNotFound
		}
		return Pair{}, fmt.Errorf("read Caddy certificates root: %w", err)
	}
	defer rootDir.Close()
	entries, err := rootDir.ReadDir()
	if err != nil {
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
		candidate, modTime, ok := loadPinnedPair(rootDir, certsRoot, issuerName, domain)
		if !ok {
			continue
		}

		score := issuerScore(issuerName)
		if score > bestScore || (score == bestScore && modTime.After(bestModTime)) {
			best = &candidate
			bestScore = score
			bestModTime = modTime
		}
	}

	if best == nil {
		return Pair{}, ErrCertificateNotFound
	}
	return *best, nil
}

// loadPinnedPair resolves <issuer>/<domain> beneath the pinned certificates
// root and reads the pair in one pass. Any symlinked intermediate directory
// fails the open (ELOOP) and the candidate is skipped.
func loadPinnedPair(rootDir *safefs.Dir, certsRoot, issuerName, domain string) (Pair, time.Time, bool) {
	issuerDir, err := rootDir.OpenDirAt(issuerName)
	if err != nil {
		return Pair{}, time.Time{}, false
	}
	defer issuerDir.Close()
	domainDir, err := issuerDir.OpenDirAt(domain)
	if err != nil {
		return Pair{}, time.Time{}, false
	}
	defer domainDir.Close()

	certData, certInfo, err := readPinnedRegular(domainDir, domain+".crt")
	if err != nil {
		return Pair{}, time.Time{}, false
	}
	keyData, _, err := readPinnedRegular(domainDir, domain+".key")
	if err != nil {
		return Pair{}, time.Time{}, false
	}
	if !isValidCertificate(certData, domain) {
		return Pair{}, time.Time{}, false
	}
	// A .key planted next to a valid .crt (or a stale half-rotation) must not
	// sync: require the pair to actually match before reporting it.
	if !certificateMatchesKey(certData, keyData) {
		return Pair{}, time.Time{}, false
	}
	pair := Pair{
		CertPath:   filepath.Join(certsRoot, issuerName, domain, domain+".crt"),
		KeyPath:    filepath.Join(certsRoot, issuerName, domain, domain+".key"),
		IssuerName: issuerName,
		CertPEM:    certData,
		KeyPEM:     keyData,
	}
	return pair, certInfo.ModTime(), true
}

// readPinnedRegular reads a single regular-file leaf inside the pinned
// directory. The open uses O_NOFOLLOW|O_NONBLOCK, so a leaf swapped for a
// symlink is rejected (ELOOP) and a swapped FIFO cannot block the read —
// non-regular types are refused after the fstat (#1083).
func readPinnedRegular(dir *safefs.Dir, name string) ([]byte, os.FileInfo, error) {
	file, err := dir.OpenFileAt(name)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("%s is not a regular file", name)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxPairMaterialBytes+1))
	if err != nil {
		return nil, nil, err
	}
	if int64(len(data)) > maxPairMaterialBytes {
		return nil, nil, fmt.Errorf("%s exceeds size limit", name)
	}
	return data, info, nil
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

// isValidCertificate verifies that data is a PEM certificate that is
// currently valid and whose SANs include domain.
func isValidCertificate(data []byte, domain string) bool {
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

// certificateMatchesKey reports whether keyPEM is the private key for the
// leaf certificate in certPEM — the same check tls.X509KeyPair performs.
func certificateMatchesKey(certPEM, keyPEM []byte) bool {
	_, err := tls.X509KeyPair(certPEM, keyPEM)
	return err == nil
}
