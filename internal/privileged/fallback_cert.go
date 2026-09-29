package privileged

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"github.com/mikkelchokolate/Veil/internal/atomicfile"
)

// fallbackCertValidity bounds the Veil-issued self-signed certificate seeded
// while an ACME issuance is still pending. The periodic cert-sync worker
// replaces it with real ACME material as soon as Caddy storage holds a pair;
// if that never happens the expiry itself makes the stale fallback visible
// (and a later sync re-seeds a fresh one) (#1168).
const fallbackCertValidity = 397 * 24 * time.Hour

// writeFallbackCaddyCert seeds OutDir with a self-signed certificate for
// request.Domain when the destination has no usable pair. The fallback lets
// a hysteria2 inbound start serving immediately while its ACME issuance is
// still pending — the status endpoint classifies it "self-signed" rather
// than trusted, and the cert-sync worker swaps in the real certificate once
// Caddy storage holds one (#1168).
//
// A destination pair that already loads and still covers the domain is left
// alone: this path must never clobber a previously synced ACME certificate
// just because Caddy storage momentarily lacks the pair.
func writeFallbackCaddyCert(request SyncCaddyCertRequest) (SyncCaddyCertResult, error) {
	certOut := filepath.Join(request.OutDir, request.Domain+".crt")
	keyOut := filepath.Join(request.OutDir, request.Domain+".key")
	if destCertUsable(certOut, keyOut, request.Domain) {
		return SyncCaddyCertResult{Found: false, CertPath: certOut, KeyPath: keyOut}, nil
	}
	if err := os.MkdirAll(request.OutDir, 0o700); err != nil {
		return SyncCaddyCertResult{}, fmt.Errorf("create cert output directory: %w", err)
	}
	// Same fail-closed ownership contract as the real sync: the material must
	// be readable by the veil-proxy protocol units (audit #522/#530).
	if effectiveUID() != 0 {
		return SyncCaddyCertResult{}, fmt.Errorf("set certificate ownership: requires root")
	}
	proxyGID, err := runtimeArtifactGID()
	if err != nil {
		return SyncCaddyCertResult{}, fmt.Errorf("set certificate ownership: %w", err)
	}
	if err := chownForProxyReadDir(request.OutDir, proxyGID); err != nil {
		return SyncCaddyCertResult{}, fmt.Errorf("set certificate directory ownership: %w", err)
	}
	certPEM, keyPEM, err := generateFallbackCertPair(request.Domain)
	if err != nil {
		return SyncCaddyCertResult{}, fmt.Errorf("generate fallback certificate: %w", err)
	}
	if err := atomicfile.Write(certOut, certPEM, 0o600, 0o700); err != nil {
		return SyncCaddyCertResult{}, fmt.Errorf("write fallback certificate: %w", err)
	}
	if err := chownForProxyReadFile(certOut, proxyGID); err != nil {
		return SyncCaddyCertResult{}, fmt.Errorf("set certificate ownership: %w", err)
	}
	if err := atomicfile.Write(keyOut, keyPEM, 0o600, 0o700); err != nil {
		return SyncCaddyCertResult{}, fmt.Errorf("write fallback key: %w", err)
	}
	if err := chownForProxyReadFile(keyOut, proxyGID); err != nil {
		return SyncCaddyCertResult{}, fmt.Errorf("set certificate key ownership: %w", err)
	}
	return SyncCaddyCertResult{Found: false, CertPath: certOut, KeyPath: keyOut, Changed: true, Fallback: true}, nil
}

// destCertUsable reports whether certOut/keyOut already hold a pair that
// loads, matches, is unexpired, and still covers domain. When it does, the
// pending ACME issuance must not displace it with a self-signed fallback
// (#1168).
func destCertUsable(certOut, keyOut, domain string) bool {
	pair, err := tls.LoadX509KeyPair(certOut, keyOut)
	if err != nil || len(pair.Certificate) == 0 {
		return false
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return false
	}
	now := time.Now()
	if now.Before(leaf.NotBefore) || now.After(leaf.NotAfter) {
		return false
	}
	if domain != "" {
		if err := leaf.VerifyHostname(domain); err != nil {
			return false
		}
	}
	return true
}

// generateFallbackCertPair creates a self-signed RSA-2048 server certificate
// for domain. The subject carries the domain so hysteria2 clients pinning on
// SANs still match, and the organization marks it as Veil fallback material
// so a human inspecting the presented chain sees what it is (#1168).
func generateFallbackCertPair(domain string) (certPEM, keyPEM []byte, err error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}
	now := time.Now().UTC()
	cert := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   domain,
			Organization: []string{"Veil self-signed fallback"},
		},
		NotBefore:   now.Add(-time.Hour),
		NotAfter:    now.Add(fallbackCertValidity),
		KeyUsage:    x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:    []string{domain},
	}
	der, err := x509.CreateCertificate(rand.Reader, &cert, &cert, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return certPEM, keyPEM, nil
}
