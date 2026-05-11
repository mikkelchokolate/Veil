package cli

import "testing"

func TestResolveServeConfigCapturesAuthTLSAndWebBasePath(t *testing.T) {
	cfg, err := NewServeSecurity(serveWorkflowOptions{
		Listen:      "127.0.0.1:2096",
		AuthToken:   "secret",
		StatePath:   "/state.json",
		ApplyRoot:   "/apply",
		KeyPath:     "/state.key",
		TLSCert:     "/cert.pem",
		TLSKey:      "/key.pem",
		WebBasePath: "secret",
	}).Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if cfg.Token != "secret" || cfg.TokenSource != "--auth-token" {
		t.Fatalf("unexpected auth config: %+v", cfg)
	}
	if !cfg.TLSEnabled || cfg.TLSSource != "--tls-cert / --tls-key" {
		t.Fatalf("unexpected TLS config: %+v", cfg)
	}
	if cfg.WebBasePath != "/secret/" {
		t.Fatalf("WebBasePath = %q", cfg.WebBasePath)
	}
}
