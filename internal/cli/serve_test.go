package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestServeCommandRejectsInvalidListenAddress(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"serve", "--listen", "bad-address"})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected invalid listen error")
	}
	if !strings.Contains(err.Error(), "listen address must be host:port") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestServeCommandRejectsInvalidPortWithAuthToken(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"serve", "--listen", "localhost:notaport", "--auth-token", "secret"})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected invalid port error when auth token is set")
	}
	if !strings.Contains(err.Error(), "invalid port") && !strings.Contains(err.Error(), "listen address") {
		t.Fatalf("expected error to mention invalid port or listen address, got: %v", err)
	}
}

func TestServeCommandRejectsEmptyHost(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"serve", "--listen", ":2096", "--auth-token", "secret"})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected empty host error")
	}
	if !strings.Contains(err.Error(), "host") && !strings.Contains(err.Error(), "listen address") {
		t.Fatalf("expected error to mention host or listen address, got: %v", err)
	}
}

func TestNewServeHTTPServerSetsTLSConfigWhenEnabled(t *testing.T) {
	server, _ := newServeHTTPServer("127.0.0.1:2096", "test", "token", "/tmp/state.json", "/tmp/apply", "/etc/veil/state.key", true, "/tmp/cert.pem", "/tmp/key.pem")
	if server.TLSConfig == nil {
		t.Fatal("expected TLSConfig to be set when TLS is enabled")
	}
	if server.TLSConfig.MinVersion != tls.VersionTLS12 {
		t.Fatalf("expected TLS 1.2 minimum, got version %d", server.TLSConfig.MinVersion)
	}
	if !server.TLSConfig.PreferServerCipherSuites {
		t.Fatal("expected PreferServerCipherSuites to be true")
	}
	if len(server.TLSConfig.CurvePreferences) < 2 {
		t.Fatal("expected X25519 and P-256 curve preferences")
	}
}

func TestNewServeHTTPServerDoesNotSetTLSConfigWhenDisabled(t *testing.T) {
	server, _ := newServeHTTPServer("127.0.0.1:2096", "test", "token", "/tmp/state.json", "/tmp/apply", "/etc/veil/state.key", false, "", "")
	if server.TLSConfig != nil {
		t.Fatal("expected TLSConfig to be nil when TLS is disabled")
	}
}

func TestResolveServeTLSFromFlags(t *testing.T) {
	enabled, source := resolveServeTLS("/tmp/cert.pem", "/tmp/key.pem")
	if !enabled {
		t.Fatal("expected TLS enabled when both cert and key flags are set")
	}
	if source != "--tls-cert / --tls-key" {
		t.Fatalf("expected flag source, got: %s", source)
	}
}

func TestResolveServeTLSRejectsCertOnly(t *testing.T) {
	enabled, _ := resolveServeTLS("/tmp/cert.pem", "")
	if enabled {
		t.Fatal("expected TLS disabled when only cert is set")
	}
}

func TestResolveServeTLSRejectsKeyOnly(t *testing.T) {
	enabled, _ := resolveServeTLS("", "/tmp/key.pem")
	if enabled {
		t.Fatal("expected TLS disabled when only key is set")
	}
}

func TestServeCommandPrintsTLSStatus(t *testing.T) {
	// Quick exit: just verify --help includes the new flags
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"serve", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	help := out.String()
	for _, want := range []string{"tls-cert", "tls-key", "HTTPS"} {
		if !strings.Contains(help, want) {
			t.Errorf("serve --help missing %q", want)
		}
	}
}

func TestNewServeTLSConfigEnforcesModernTLS(t *testing.T) {
	cfg := newServeTLSConfig()
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Fatalf("expected TLS 1.2 min, got %d", cfg.MinVersion)
	}
	// Ensure TLS 1.0 and 1.1 are not in allowed versions
	for _, ver := range []uint16{tls.VersionTLS10, tls.VersionTLS11} {
		if cfg.MinVersion <= ver {
			t.Fatalf("TLS version 0x%04x should be rejected by MinVersion=%d", ver, cfg.MinVersion)
		}
	}
	// Verify only AEAD cipher suites
	for _, cs := range cfg.CipherSuites {
		switch cs {
		case tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256:
			// valid
		default:
			t.Fatalf("unexpected cipher suite: 0x%04x", cs)
		}
	}
}

func TestServeTLSIntegration(t *testing.T) {
	// Start a real HTTPS server, make a request, verify TLS is active.
	// We use self-signed certs generated on the fly.
	cert, err := generateSelfSignedCert()
	if err != nil {
		t.Fatalf("failed to generate self-signed cert: %v", err)
	}

	// Create a temp state.json so healthz returns 200.
	stateFile, err := writeTempFile("veil-test-state-*.json", []byte("{}"))
	if err != nil {
		t.Fatalf("failed to create temp state file: %v", err)
	}
	defer os.Remove(stateFile)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{
		"serve",
		"--listen", "127.0.0.1:13096",
		"--auth-token", "test",
		"--tls-cert", cert.certFile,
		"--tls-key", cert.keyFile,
		"--state", stateFile,
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.Execute()
	}()

	// Wait for server startup.
	select {
	case err := <-errCh:
		t.Fatalf("server exited before test: %v", err)
	case <-time.After(500 * time.Millisecond):
	}

	// Make an HTTPS request with a client that trusts our self-signed cert.
	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: 5 * time.Second,
	}
	resp, err := httpClient.Get("https://127.0.0.1:13096/healthz")
	if err != nil {
		t.Fatalf("HTTPS healthz request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.TLS == nil {
		t.Fatal("expected TLS handshake in response")
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("expected nil error after shutdown, got: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down")
	}
}

// selfSignedCert holds paths to temporary cert/key files.
type selfSignedCert struct {
	certFile string
	keyFile  string
}

// generateSelfSignedCert creates a temporary self-signed certificate
// using crypto/tls internal helpers.
func generateSelfSignedCert() (*selfSignedCert, error) {
	cert, key, err := generateTestCertificate()
	if err != nil {
		return nil, err
	}
	certFile, err := writeTempFile("veil-test-cert-*.pem", cert)
	if err != nil {
		return nil, err
	}
	keyFile, err := writeTempFile("veil-test-key-*.pem", key)
	if err != nil {
		return nil, err
	}
	return &selfSignedCert{certFile: certFile, keyFile: keyFile}, nil
}

// generateTestCertificate creates a self-signed TLS certificate and key pair
// for use in integration tests. Returns PEM-encoded cert and key bytes.
func generateTestCertificate() (certPEM, keyPEM []byte, err error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "veil-test"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(1 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return certPEM, keyPEM, nil
}

// writeTempFile writes data to a temporary file with the given name pattern.
func writeTempFile(pattern string, data []byte) (string, error) {
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return "", err
	}
	return f.Name(), nil
}

func TestServeGracefulShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"serve", "--listen", "127.0.0.1:12096", "--auth-token", "test-token"})

	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.Execute()
	}()

	// Wait for the server to start accepting connections.
	select {
	case err := <-errCh:
		t.Fatalf("server exited before shutdown signal: %v", err)
	case <-time.After(500 * time.Millisecond):
	}

	// Cancel the context to trigger graceful shutdown.
	cancel()

	// The server should shut down within the drain timeout plus a margin.
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("expected nil error after graceful shutdown, got: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("server did not shut down within expected time")
	}
}
