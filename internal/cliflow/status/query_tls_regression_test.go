package status

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func panelLikeCertificate(t *testing.T) (tls.Certificate, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "Veil Panel"},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "tls.crt")
	if err := os.WriteFile(path, certPEM, 0o644); err != nil {
		t.Fatal(err)
	}
	return cert, path
}

func startPanelTLSServer(t *testing.T, cert tls.Certificate, handler http.Handler) *httptest.Server {
	t.Helper()
	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	server.StartTLS()
	t.Cleanup(server.Close)
	return server
}

func TestFetchTrustsConfiguredPanelTLSCertificate(t *testing.T) {
	isolateListenConfig(t)
	cert, path := panelLikeCertificate(t)
	t.Setenv("VEIL_TLS_CERT", path)
	want := &Response{Version: "0.7.0", Mode: "server"}
	server := startPanelTLSServer(t, cert, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(want)
	}))

	got, err := Fetch(context.Background(), server.URL+"/api/status", "")
	if err != nil {
		t.Fatalf("self-signed local panel TLS should be trusted when configured: %v", err)
	}
	if got.Version != want.Version {
		t.Fatalf("Fetch = %+v, want %+v", got, want)
	}
}

func TestFetchRejectsUnknownTLSCertificate(t *testing.T) {
	isolateListenConfig(t)
	_, path := panelLikeCertificate(t)
	t.Setenv("VEIL_TLS_CERT", path)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	if _, err := Fetch(context.Background(), server.URL+"/api/status", ""); err == nil {
		t.Fatal("unknown TLS certificate must be rejected")
	}
}

func TestHTTPClientDoesNotSkipVerify(t *testing.T) {
	isolateListenConfig(t)
	_, path := panelLikeCertificate(t)
	t.Setenv("VEIL_TLS_CERT", path)
	client := HTTPClient("https://127.0.0.1:2096/healthz")
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", client.Transport)
	}
	if transport.TLSClientConfig == nil || transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("status/update client must not disable TLS verification")
	}
	if transport.TLSClientConfig.RootCAs == nil {
		t.Fatal("expected a trust pool that includes the configured panel certificate")
	}
}

func TestFetchTrustsPanelTLSCertFromInstalledEnvFile(t *testing.T) {
	isolateListenConfig(t)
	cert, path := panelLikeCertificate(t)
	envPath := filepath.Join(t.TempDir(), "veil.env")
	if err := os.WriteFile(envPath, []byte("VEIL_TLS_CERT="+path+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	installedEnvFile = envPath
	t.Setenv("VEIL_TLS_CERT", "")
	want := &Response{Version: "installed-tls", Mode: "server"}
	server := startPanelTLSServer(t, cert, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(want)
	}))
	got, err := Fetch(context.Background(), server.URL+"/api/status", "")
	if err != nil {
		t.Fatalf("installed panel TLS cert from veil.env should be trusted: %v", err)
	}
	if got.Version != want.Version {
		t.Fatalf("Fetch = %+v, want %+v", got, want)
	}
}

func TestHTTPClientKeepsDefaultVerificationWithoutPanelCert(t *testing.T) {
	isolateListenConfig(t)
	t.Setenv("VEIL_TLS_CERT", "")
	if client := HTTPClient("https://example.com/api/status"); client != http.DefaultClient {
		t.Fatal("https without a local panel certificate should keep default public CA verification")
	}
	if client := HTTPClient("http://127.0.0.1:2096/api/status"); client != http.DefaultClient {
		t.Fatal("Caddy-loopback HTTP should keep the default client")
	}
}
