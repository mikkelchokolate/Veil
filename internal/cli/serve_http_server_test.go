package cli

import (
	"crypto/tls"
	"testing"
)

func TestServeHTTPServerBuildsTLSConfiguredServer(t *testing.T) {
	server, reloader := NewServeHTTPServer(serveHTTPServerOptions{
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
