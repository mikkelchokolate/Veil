package serve

import (
	"context"
	"crypto/tls"
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
)

func startRecoveryTestHelper(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "helper.sock")
	server := privileged.NewServer(privileged.NewLocalAdapter(privileged.DefaultPolicy(), privileged.Executor{
		RecoverKeyRotation: func(context.Context) error { return nil },
	}))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.ServeUnix(ctx, path, uint32(os.Getuid()), true) }()
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

func TestServeHTTPServerReportsMissingHelperSocketAsRepairable(t *testing.T) {
	server, _ := NewHTTPServer(HTTPServerOptions{
		Listen:       "127.0.0.1:2096",
		Version:      "test",
		AuthToken:    "token",
		StatePath:    filepath.Join(t.TempDir(), "state.json"),
		ApplyRoot:    filepath.Join(t.TempDir(), "apply"),
		KeyPath:      filepath.Join(t.TempDir(), "state.key"),
		HelperSocket: filepath.Join(t.TempDir(), "helper.sock"),
		WebBasePath:  "/",
	}).Build()
	request := httptest.NewRequest(http.MethodPost, "/api/services/veil/restart", strings.NewReader(`{"confirm":true}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Veil-Token", "token")
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), string(privileged.ErrorOperationFailed)) {
		t.Fatalf("missing helper error envelope: %s", response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "veil-helper.socket") {
		t.Fatalf("missing repair hint: %s", response.Body.String())
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
	server, _ := NewHTTPServer(HTTPServerOptions{
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
}
