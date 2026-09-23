package api

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/caddycert"
	veilruntime "github.com/mikkelchokolate/Veil/internal/runtime"
)

func TestTLSEndpointRejectsNonGet(t *testing.T) {
	r, _ := newTestRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodPost, "/api/tls", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestTLSEndpointReturnsCertInfoWhenConfigured(t *testing.T) {
	// Create a self-signed cert for testing
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")

	// Generate a self-signed cert using openssl if available, else write a minimal PEM
	certPEM, _ := generateSelfSignedCert()
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	_ = keyPath

	t.Setenv("VEIL_TLS_CERT", certPath)

	r, _ := newTestRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/api/tls", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var info veilruntime.TLSCertInfo
	if err := json.NewDecoder(w.Body).Decode(&info); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !info.Valid {
		t.Errorf("expected valid cert, got error: %s", info.Error)
	}
	if info.Path != certPath {
		t.Errorf("expected path %s, got %s", certPath, info.Path)
	}
	if info.DaysRemaining <= 0 {
		t.Errorf("expected positive days remaining, got %d", info.DaysRemaining)
	}
}

func TestTLSEndpointReturnsErrorWhenNoCertConfigured(t *testing.T) {
	t.Setenv("VEIL_TLS_CERT", "")
	r, _ := newTestRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/api/tls", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var info veilruntime.TLSCertInfo
	json.NewDecoder(w.Body).Decode(&info)
	if info.Valid {
		t.Error("expected invalid cert when none configured")
	}
	if info.Error == "" {
		t.Error("expected error message when no cert configured")
	}
}

// When the panel runs behind the managed Caddy edge and no VEIL_TLS_CERT is
// configured, /api/tls must report the Caddy-managed certificate together with
// the issuer that actually served it — an ACME failure that fell back to
// Caddy's internal CA is a degraded state and must not look identical to a
// trusted issuance or to "no certificate" (#906).
func TestTLSEndpointReportsCaddyIssuerSource(t *testing.T) {
	dir := t.TempDir()
	certPEM, _ := generateSelfSignedCert()
	certPath := filepath.Join(dir, "panel.example.com.crt")
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	t.Setenv("VEIL_TLS_CERT", "")

	oldLookup := caddyPanelCertPair
	caddyPanelCertPair = func(caddyDataDir, domain string) (caddycert.Pair, error) {
		if domain != "panel.example.com" {
			t.Fatalf("caddy cert lookup domain = %q, want panel.example.com", domain)
		}
		return caddycert.Pair{CertPath: certPath, KeyPath: certPath + ".key", IssuerName: "local"}, nil
	}
	t.Cleanup(func() { caddyPanelCertPair = oldLookup })

	r, _ := newTestRouter(ServerInfo{Version: "test", PanelAccess: "caddy", Domain: "panel.example.com"})
	req := httptest.NewRequest(http.MethodGet, "/api/tls", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var info veilruntime.TLSCertInfo
	if err := json.NewDecoder(w.Body).Decode(&info); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !info.Valid {
		t.Fatalf("expected valid caddy-managed cert, got %+v", info)
	}
	if info.Path != certPath || info.ManagedBy != "caddy" {
		t.Fatalf("expected caddy-managed cert path, got %+v", info)
	}
	if info.IssuerSource != "local" || info.IssuerKind != "internal" {
		t.Fatalf("internal-CA fallback must be visible as issuerSource=local issuerKind=internal, got %+v", info)
	}
}

// A publicly-trusted ACME issuer directory must surface as issuerKind=acme so
// the internal fallback stays distinguishable (#906).
func TestTLSEndpointReportsCaddyACMEIssuer(t *testing.T) {
	dir := t.TempDir()
	certPEM, _ := generateSelfSignedCert()
	certPath := filepath.Join(dir, "panel.example.com.crt")
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	t.Setenv("VEIL_TLS_CERT", "")

	oldLookup := caddyPanelCertPair
	caddyPanelCertPair = func(caddyDataDir, domain string) (caddycert.Pair, error) {
		return caddycert.Pair{CertPath: certPath, IssuerName: "acme-v02.api.letsencrypt.org-directory"}, nil
	}
	t.Cleanup(func() { caddyPanelCertPair = oldLookup })

	r, _ := newTestRouter(ServerInfo{Version: "test", PanelAccess: "caddy", Domain: "panel.example.com"})
	req := httptest.NewRequest(http.MethodGet, "/api/tls", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var info veilruntime.TLSCertInfo
	if err := json.NewDecoder(w.Body).Decode(&info); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if info.ManagedBy != "caddy" || info.IssuerKind != "acme" || info.IssuerSource != "acme-v02.api.letsencrypt.org-directory" {
		t.Fatalf("expected ACME issuer source, got %+v", info)
	}
}

// When VEIL_TLS_CERT points at an explicit certificate the env path wins and
// the Caddy store must not be consulted (#906).
func TestTLSEndpointEnvCertSkipsCaddyLookup(t *testing.T) {
	dir := t.TempDir()
	certPEM, _ := generateSelfSignedCert()
	certPath := filepath.Join(dir, "explicit.pem")
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	t.Setenv("VEIL_TLS_CERT", certPath)

	oldLookup := caddyPanelCertPair
	caddyPanelCertPair = func(caddyDataDir, domain string) (caddycert.Pair, error) {
		t.Fatal("caddy cert lookup must not run when VEIL_TLS_CERT is configured")
		return caddycert.Pair{}, nil
	}
	t.Cleanup(func() { caddyPanelCertPair = oldLookup })

	r, _ := newTestRouter(ServerInfo{Version: "test", PanelAccess: "caddy", Domain: "panel.example.com"})
	req := httptest.NewRequest(http.MethodGet, "/api/tls", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var info veilruntime.TLSCertInfo
	if err := json.NewDecoder(w.Body).Decode(&info); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !info.Valid || info.Path != certPath || info.ManagedBy != "" {
		t.Fatalf("expected explicit VEIL_TLS_CERT info only, got %+v", info)
	}
}

// generateSelfSignedCert creates a self-signed certificate valid for 365 days.
func generateSelfSignedCert() (certPEM, keyPEM []byte) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test.example.com"},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		DNSNames:     []string{"test.example.com"},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyBytes, _ := x509.MarshalECPrivateKey(key)
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})
	return
}
