package serve

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/privileged"
	"golang.org/x/crypto/acme"
)

func startRecoveryTestHelper(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "helper.sock")
	server := privileged.NewServer(privileged.NewLocalAdapter(privileged.DefaultPolicy(), privileged.Executor{
		RecoverKeyRotation: func(context.Context) error { return nil },
	}))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- server.ServeUnix(ctx, path, privileged.PeerPolicy{AllowedUID: uint32(os.Getuid()), AllowRoot: true})
	}()
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("test helper socket did not start")
		}
		time.Sleep(time.Millisecond)
	}
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("stop test helper: %v", err)
		}
	})
	return path
}

func TestServeHTTPServerBuildsTLSConfiguredServer(t *testing.T) {
	server, reloader := NewHTTPServer(HTTPServerOptions{
		Listen:      "127.0.0.1:2096",
		Version:     "test",
		AuthToken:   "token",
		StatePath:   "/tmp/state.json",
		ApplyRoot:   "/tmp/apply",
		KeyPath:     "/tmp/state.key",
		TLSEnabled:  true,
		TLSCert:     "/tmp/cert.pem",
		TLSKey:      "/tmp/key.pem",
		WebBasePath: "/",
	}).Build()
	if server == nil || reloader == nil {
		t.Fatalf("server=%v reloader=%v", server, reloader)
	}
	if server.TLSConfig == nil {
		t.Fatalf("expected TLS config")
	}
	if server.TLSConfig.MinVersion != tls.VersionTLS12 {
		t.Fatalf("MinVersion=%d", server.TLSConfig.MinVersion)
	}
}

func TestServeHTTPServerLoadsAuthWithoutHelperSocket(t *testing.T) {
	root := t.TempDir()
	server, reloader := NewHTTPServer(HTTPServerOptions{
		Listen:       "127.0.0.1:2096",
		Version:      "test",
		StatePath:    filepath.Join(root, "state.json"),
		ApplyRoot:    filepath.Join(root, "apply"),
		KeyPath:      filepath.Join(root, "state.key"),
		HelperSocket: filepath.Join(root, "helper.sock"),
		WebBasePath:  "/",
		SetupAllowed: true,
	}).Build()
	if closer, ok := reloader.(interface{ Close() error }); ok {
		t.Cleanup(func() { _ = closer.Close() })
	}
	request := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)
	if response.Code == http.StatusServiceUnavailable {
		t.Fatalf("missing helper fail-closed public auth: %d %s", response.Code, response.Body.String())
	}
	if response.Code != http.StatusOK {
		t.Fatalf("auth status=%d body=%s", response.Code, response.Body.String())
	}
	// The endpoint must answer the auth-status contract, not just any 200:
	// valid JSON with an explicit authenticated flag and the resolution
	// method. On a private listen with no users yet, the effective identity
	// is the dev-anonymous administrator — the same identity the auth
	// middleware would grant — so authenticated=true here is the honest
	// answer, not a missing-helper failure.
	var status struct {
		Authenticated bool   `json:"authenticated"`
		Username      string `json:"username"`
		Role          string `json:"role"`
		AuthMethod    string `json:"authMethod"`
	}
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		t.Fatalf("auth status is not JSON: %v (%s)", err, response.Body.String())
	}
	if !status.Authenticated || status.AuthMethod != "dev-anonymous" || status.Role != "admin" {
		t.Fatalf("expected dev-anonymous admin auth status on a private no-user panel, got %s", response.Body.String())
	}
}

func TestServeHTTPServerReportsMissingHelperSocketAsRepairable(t *testing.T) {
	root := t.TempDir()
	server, reloader := NewHTTPServer(HTTPServerOptions{
		Listen:       "127.0.0.1:2096",
		Version:      "test",
		AuthToken:    "token",
		StatePath:    filepath.Join(root, "state.json"),
		ApplyRoot:    filepath.Join(root, "apply"),
		KeyPath:      filepath.Join(root, "state.key"),
		HelperSocket: filepath.Join(root, "helper.sock"),
		WebBasePath:  "/",
	}).Build()
	if closer, ok := reloader.(interface{ Close() error }); ok {
		t.Cleanup(func() { _ = closer.Close() })
	}
	request := httptest.NewRequest(http.MethodPost, "/api/services/veil/restart", strings.NewReader(`{"confirm":true}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Veil-Token", "token")
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if strings.Contains(body, string(privileged.ErrorOperationFailed)) && strings.Contains(body, "veil-helper.socket") {
		return
	}
	if !strings.Contains(body, `"code":"dependency_unavailable"`) && !strings.Contains(body, "privileged helper is unavailable") {
		t.Fatalf("missing helper error envelope: %s", body)
	}
}

func TestEnvironmentHelperSocketUsesFlagEnvAndDefault(t *testing.T) {
	env := NewEnvironment()
	if path, source := env.HelperSocket("/custom/helper.sock"); path != "/custom/helper.sock" || source != "--helper-socket" {
		t.Fatalf("flag path=%q source=%q", path, source)
	}
	t.Setenv("VEIL_HELPER_SOCKET", "/env/helper.sock")
	if path, source := env.HelperSocket(""); path != "/env/helper.sock" || source != "VEIL_HELPER_SOCKET" {
		t.Fatalf("env path=%q source=%q", path, source)
	}
	t.Setenv("VEIL_HELPER_SOCKET", "")
	if path, source := env.HelperSocket(""); path != privileged.DefaultSocketPath || source != "default" {
		t.Fatalf("default path=%q source=%q", path, source)
	}
}

func TestServeHTTPServerInjectsLiveHostValidator(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	root := t.TempDir()
	helperSocket := startRecoveryTestHelper(t)
	server, _ := NewHTTPServer(HTTPServerOptions{
		Listen:       "127.0.0.1:2096",
		Version:      "test",
		AuthToken:    "token",
		StatePath:    filepath.Join(root, "state.json"),
		ApplyRoot:    filepath.Join(root, "apply"),
		KeyPath:      filepath.Join(root, "state.key"),
		HelperSocket: helperSocket,
		WebBasePath:  "/",
	}).Build()

	body := `{"settings":{"panelListen":"127.0.0.1:2096","mode":"server"},"inbounds":[{"name":"edge","protocol":"mieru","transport":"tcp","port":` + strconv.Itoa(port) + `,"enabled":true,"password":"secret"}],"warp":{}}`
	request := httptest.NewRequest(http.MethodPost, "/api/validation", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Veil-Token", "token")
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"code":"port_in_use"`) {
		t.Fatalf("production validation did not inspect live port: %s", response.Body.String())
	}
}

func TestServeHTTPServerBuildsAutoTLSServer(t *testing.T) {
	server, reloader := NewHTTPServer(HTTPServerOptions{
		Listen:          "0.0.0.0:443",
		Version:         "test",
		AuthToken:       "token",
		StatePath:       filepath.Join(t.TempDir(), "state.json"),
		ApplyRoot:       filepath.Join(t.TempDir(), "apply"),
		KeyPath:         filepath.Join(t.TempDir(), "state.key"),
		TLSEnabled:      true,
		AutoTLSDomain:   "example.com",
		AutoTLSEmail:    "admin@example.com",
		AutoTLSCacheDir: filepath.Join(t.TempDir(), "autocert"),
		WebBasePath:     "/",
	}).Build()
	if server == nil || reloader == nil {
		t.Fatalf("server=%v reloader=%v", server, reloader)
	}
	if server.TLSConfig == nil {
		t.Fatalf("expected TLS config for auto-tls")
	}
	if server.TLSConfig.MinVersion != tls.VersionTLS12 {
		t.Fatalf("MinVersion=%d", server.TLSConfig.MinVersion)
	}
	if server.TLSConfig.GetCertificate == nil {
		t.Fatalf("expected auto-tls GetCertificate")
	}
	if !tlsNextProtoContains(server.TLSConfig.NextProtos, acme.ALPNProto) {
		t.Fatalf("auto-tls NextProtos=%v missing %q", server.TLSConfig.NextProtos, acme.ALPNProto)
	}
}

func TestServeHTTPServerFileTLSDoesNotAdvertiseACMEALPN(t *testing.T) {
	server, _ := NewHTTPServer(HTTPServerOptions{
		Listen:      "127.0.0.1:2096",
		Version:     "test",
		AuthToken:   "token",
		StatePath:   filepath.Join(t.TempDir(), "state.json"),
		ApplyRoot:   filepath.Join(t.TempDir(), "apply"),
		KeyPath:     filepath.Join(t.TempDir(), "state.key"),
		TLSEnabled:  true,
		TLSCert:     "/tmp/cert.pem",
		TLSKey:      "/tmp/key.pem",
		WebBasePath: "/",
	}).Build()
	if server.TLSConfig == nil {
		t.Fatalf("expected TLS config")
	}
	if tlsNextProtoContains(server.TLSConfig.NextProtos, acme.ALPNProto) {
		t.Fatalf("file TLS advertised ACME ALPN: %v", server.TLSConfig.NextProtos)
	}
}

func TestServeHTTPServerBuildsPlainServer(t *testing.T) {
	server, reloader := NewHTTPServer(HTTPServerOptions{
		Listen:      "127.0.0.1:2096",
		Version:     "test",
		AuthToken:   "token",
		StatePath:   filepath.Join(t.TempDir(), "state.json"),
		ApplyRoot:   filepath.Join(t.TempDir(), "apply"),
		KeyPath:     filepath.Join(t.TempDir(), "state.key"),
		WebBasePath: "/",
	}).Build()
	if server == nil || reloader == nil {
		t.Fatalf("server=%v reloader=%v", server, reloader)
	}
	if server.TLSConfig != nil {
		t.Fatalf("expected no TLS config for plain server")
	}
}

func TestServeHTTPServerUsesDefaultHelperSocket(t *testing.T) {
	// No HelperSocket option: Build must fall back to
	// privileged.DefaultSocketPath (/run/veil/helper.sock). The socket is
	// absent in tests, so a privileged request must surface the dedicated
	// helper-unavailable envelope — which the API only emits when the dial
	// error names the helper.sock path. A missing default ("") would instead
	// produce a generic privileged-operation failure.
	server, reloader := NewHTTPServer(HTTPServerOptions{
		Listen:      "127.0.0.1:2096",
		Version:     "test",
		AuthToken:   "token",
		StatePath:   filepath.Join(t.TempDir(), "state.json"),
		ApplyRoot:   filepath.Join(t.TempDir(), "apply"),
		KeyPath:     filepath.Join(t.TempDir(), "state.key"),
		WebBasePath: "/",
	}).Build()
	if server == nil {
		t.Fatalf("expected server")
	}
	if closer, ok := reloader.(interface{ Close() error }); ok {
		t.Cleanup(func() { _ = closer.Close() })
	}
	// /api/status dials the helper synchronously (no apply-tracking fence in
	// front of it), so the dial error names the configured socket path. With
	// the default applied, the API emits the repairable helper-unavailable
	// envelope that mentions veil-helper.socket; had Build left the path
	// empty, the dial error would not name helper.sock and the response
	// would be a generic privileged-operation failure.
	request := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	request.Header.Set("X-Veil-Token", "token")
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("default helper socket status=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "veil-helper.socket") {
		t.Fatalf("default helper socket did not produce the helper-unavailable envelope: %s", response.Body.String())
	}
}
