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
)

func TestTLSEndpointRejectsNonGet(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test"})
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

	r, _ := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/api/tls", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var info TLSCertInfo
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
	r, _ := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/api/tls", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var info TLSCertInfo
	json.NewDecoder(w.Body).Decode(&info)
	if info.Valid {
		t.Error("expected invalid cert when none configured")
	}
	if info.Error == "" {
		t.Error("expected error message when no cert configured")
	}
}

func TestReadTLSCertWithInvalidFile(t *testing.T) {
	info := readTLSCert("/nonexistent/cert.pem")
	if info.Valid {
		t.Error("expected invalid for nonexistent file")
	}
	if info.Error == "" {
		t.Error("expected error for nonexistent file")
	}
}

func TestReadTLSCertWithEmptyPath(t *testing.T) {
	info := readTLSCert("")
	if info.Valid {
		t.Error("expected invalid for empty path")
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
