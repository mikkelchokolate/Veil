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
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	serveflow "github.com/mikkelchokolate/Veil/internal/cliflow/serve"
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
	server, _ := newServeHTTPServer("127.0.0.1:2096", "test", "token", "/tmp/state.json", "/tmp/apply", "/etc/veil/state.key", true, "/tmp/cert.pem", "/tmp/key.pem", "/")
	if server.TLSConfig == nil {
		t.Fatal("expected TLSConfig to be set when TLS is enabled")
	}
	if server.TLSConfig.MinVersion != tls.VersionTLS12 {
		t.Fatalf("expected TLS 1.2 minimum, got version %d", server.TLSConfig.MinVersion)
	}
	if len(server.TLSConfig.CipherSuites) == 0 {
		t.Fatal("expected explicit cipher suites to be configured")
	}
	if len(server.TLSConfig.CurvePreferences) < 2 {
		t.Fatal("expected X25519 and P-256 curve preferences")
	}
}

func TestNewServeHTTPServerDoesNotSetTLSConfigWhenDisabled(t *testing.T) {
	server, _ := newServeHTTPServer("127.0.0.1:2096", "test", "token", "/tmp/state.json", "/tmp/apply", "/etc/veil/state.key", false, "", "", "/")
	if server.TLSConfig != nil {
		t.Fatal("expected TLSConfig to be nil when TLS is disabled")
	}
}

func TestResolveServeTLSFromFlags(t *testing.T) {
	enabled, source := serveflow.NewEnvironment().TLS("/tmp/cert.pem", "/tmp/key.pem")
	if !enabled {
		t.Fatal("expected TLS enabled when both cert and key flags are set")
	}
	if source != "--tls-cert / --tls-key" {
		t.Fatalf("expected flag source, got: %s", source)
	}
}

func TestResolveServeTLSRejectsCertOnly(t *testing.T) {
	enabled, _ := serveflow.NewEnvironment().TLS("/tmp/cert.pem", "")
	if enabled {
		t.Fatal("expected TLS disabled when only cert is set")
	}
}

func TestResolveServeTLSRejectsKeyOnly(t *testing.T) {
	enabled, _ := serveflow.NewEnvironment().TLS("", "/tmp/key.pem")
	if enabled {
		t.Fatal("expected TLS disabled when only key is set")
	}
}

// freeLoopbackPort reserves an ephemeral port so serve tests do not collide
// on fixed port numbers.
func freeLoopbackPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// runServeUntilStartup launches the real serve workflow, waits until the
// startup banner is printed, then cancels. It returns the captured output.
func runServeUntilStartup(t *testing.T, extraArgs ...string) string {
	t.Helper()
	root := t.TempDir()
	listen := fmt.Sprintf("127.0.0.1:%d", freeLoopbackPort(t))
	stateFile := filepath.Join(root, "state.json")
	if err := os.WriteFile(stateFile, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(ctx)
	args := []string{
		"serve",
		"--listen", listen,
		"--auth-token", "test",
		"--state", stateFile,
		"--key-path", filepath.Join(root, "state.key"),
		"--apply-root", filepath.Join(root, "apply"),
		"--helper-socket", filepath.Join(root, "helper.sock"),
	}
	cmd.SetArgs(append(args, extraArgs...))

	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.Execute()
	}()

	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(out.String(), "TLS:") {
		select {
		case err := <-errCh:
			t.Fatalf("serve exited before printing TLS status: %v\n%s", err, out.String())
		default:
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("timed out waiting for TLS status line\n%s", out.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("serve shutdown error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not shut down")
	}
	return out.String()
}

func TestServeCommandPrintsTLSStatus(t *testing.T) {
	// Exercise the real serve-start path for both modes; the banner must
	// report the actual negotiated TLS state, not just document the flags.
	t.Run("disabled", func(t *testing.T) {
		out := runServeUntilStartup(t)
		if !strings.Contains(out, "TLS: disabled") {
			t.Fatalf("expected \"TLS: disabled\" in startup output:\n%s", out)
		}
		if !strings.Contains(out, "http://") {
			t.Fatalf("expected http listen URL in startup output:\n%s", out)
		}
	})
	t.Run("enabled", func(t *testing.T) {
		cert, err := generateSelfSignedCert()
		if err != nil {
			t.Fatalf("self-signed cert: %v", err)
		}
		out := runServeUntilStartup(t, "--tls-cert", cert.certFile, "--tls-key", cert.keyFile)
		if !strings.Contains(out, "TLS: enabled (--tls-cert / --tls-key)") {
			t.Fatalf("expected \"TLS: enabled (--tls-cert / --tls-key)\":\n%s", out)
		}
		if !strings.Contains(out, "https://") {
			t.Fatalf("expected https listen URL in startup output:\n%s", out)
		}
	})
}

func TestServeCommandHelpDocumentsTLSFlags(t *testing.T) {
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
	cfg := serveflow.NewTLSConfig()
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Fatalf("expected TLS 1.2 min, got %d", cfg.MinVersion)
	}
	// Ensure TLS 1.0 and 1.1 are not in allowed versions
	for _, ver := range []uint16{tls.VersionTLS10, tls.VersionTLS11} {
		if cfg.MinVersion <= ver {
			t.Fatalf("TLS version 0x%04x should be rejected by MinVersion=%d", ver, cfg.MinVersion)
		}
	}
	// The cipher-suite pin must actually contain entries — ranging over an
	// empty list would pass vacuously while offering no AEAD guarantee.
	if len(cfg.CipherSuites) == 0 {
		t.Fatal("expected non-empty AEAD cipher suite list")
	}
	for _, cs := range cfg.CipherSuites {
		switch cs {
		case tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256:
			// valid AEAD suite
		default:
			t.Fatalf("unexpected cipher suite: 0x%04x", cs)
		}
	}
	if len(cfg.CurvePreferences) == 0 {
		t.Fatal("expected explicit curve preferences")
	}
	for _, curve := range cfg.CurvePreferences {
		switch curve {
		case tls.X25519, tls.CurveP256:
			// modern curves only
		default:
			t.Fatalf("unexpected curve preference: 0x%04x", curve)
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

	root := t.TempDir()
	stateFile := filepath.Join(root, "state.json")
	if err := os.WriteFile(stateFile, []byte("{}"), 0o600); err != nil {
		t.Fatalf("failed to create temp state file: %v", err)
	}

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
		"--key-path", filepath.Join(root, "state.key"),
		"--apply-root", filepath.Join(root, "apply"),
		"--helper-socket", filepath.Join(root, "helper.sock"),
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.Execute()
	}()

	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: 2 * time.Second,
	}
	var resp *http.Response
	deadline := time.Now().Add(5 * time.Second)
	for {
		select {
		case err := <-errCh:
			t.Fatalf("server exited before test: %v\n%s", err, out.String())
		default:
		}
		resp, err = httpClient.Get("https://127.0.0.1:13096/healthz")
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("HTTPS healthz request failed: %v\n%s", err, out.String())
		}
		time.Sleep(50 * time.Millisecond)
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

func TestResolveServeTLSFromEnv(t *testing.T) {
	t.Setenv("VEIL_TLS_CERT", "/env/cert.pem")
	t.Setenv("VEIL_TLS_KEY", "/env/key.pem")
	enabled, source := serveflow.NewEnvironment().TLS("", "")
	if !enabled {
		t.Fatal("expected TLS enabled from env vars")
	}
	if source != "VEIL_TLS_CERT / VEIL_TLS_KEY" {
		t.Fatalf("expected env source, got: %s", source)
	}
}

func TestResolveServeTLSEnvFlagPrecedence(t *testing.T) {
	// Flags should take precedence over env vars
	t.Setenv("VEIL_TLS_CERT", "/env/cert.pem")
	t.Setenv("VEIL_TLS_KEY", "/env/key.pem")
	enabled, source := serveflow.NewEnvironment().TLS("/flag/cert.pem", "/flag/key.pem")
	if !enabled {
		t.Fatal("expected TLS enabled from flags")
	}
	if source != "--tls-cert / --tls-key" {
		t.Fatalf("expected flag source when both are set, got: %s", source)
	}
}

func TestResolveServeTLSEnvOnlyOneVarSet(t *testing.T) {
	// Only cert in env, no key — should be disabled
	t.Setenv("VEIL_TLS_CERT", "/env/cert.pem")
	enabled, _ := serveflow.NewEnvironment().TLS("", "")
	if enabled {
		t.Fatal("expected TLS disabled when only VEIL_TLS_CERT is set")
	}
}

func TestResolveServeKeyPathFromEnv(t *testing.T) {
	t.Setenv("VEIL_KEY_PATH", "/custom/key.path")
	path, source := serveflow.NewEnvironment().KeyPath("")
	if path != "/custom/key.path" {
		t.Fatalf("expected /custom/key.path from env, got: %s", path)
	}
	if source != "VEIL_KEY_PATH" {
		t.Fatalf("expected VEIL_KEY_PATH source, got: %s", source)
	}
}

func TestResolveServeKeyPathDefault(t *testing.T) {
	t.Setenv("VEIL_KEY_PATH", "")
	path, source := serveflow.NewEnvironment().KeyPath("")
	expected := "/etc/veil/state.key"
	if runtime.GOOS == "windows" {
		pd := os.Getenv("ProgramData")
		if pd == "" {
			pd = `C:\ProgramData`
		}
		expected = filepath.Join(pd, "Veil", "state.key")
	}
	if path != expected {
		t.Fatalf("expected default key path, got: %s", path)
	}
	if source != "default" {
		t.Fatalf("expected default source, got: %s", source)
	}
}

func TestResolveServeKeyPathFromFlag(t *testing.T) {
	path, source := serveflow.NewEnvironment().KeyPath("/custom/key.path")
	if path != "/custom/key.path" {
		t.Fatalf("expected flag key path, got: %s", path)
	}
	if source != "--key-path" {
		t.Fatalf("expected --key-path source, got: %s", source)
	}
}

func newServeHTTPServer(listen string, version string, authToken string, statePath string, applyRoot string, keyPath string, tlsEnabled bool, tlsCert string, tlsKey string, webBasePath string) (*http.Server, any) {
	server, reloader := serveflow.NewHTTPServer(serveflow.HTTPServerOptions{
		Listen:      listen,
		Version:     version,
		AuthToken:   authToken,
		StatePath:   statePath,
		ApplyRoot:   applyRoot,
		KeyPath:     keyPath,
		TLSEnabled:  tlsEnabled,
		TLSCert:     tlsCert,
		TLSKey:      tlsKey,
		WebBasePath: webBasePath,
	}).Build()
	return server, reloader
}
