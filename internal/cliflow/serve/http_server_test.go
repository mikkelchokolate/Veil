package serve

import (
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

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

func TestServeHTTPServerInjectsLiveHostValidator(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	root := t.TempDir()
	server, _ := NewHTTPServer(HTTPServerOptions{
		Listen:      "127.0.0.1:2096",
		Version:     "test",
		AuthToken:   "token",
		StatePath:   filepath.Join(root, "state.json"),
		ApplyRoot:   filepath.Join(root, "apply"),
		KeyPath:     filepath.Join(root, "state.key"),
		WebBasePath: "/",
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
