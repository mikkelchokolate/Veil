package cli

import "testing"

func TestServeSecurityResolvesSafeConfig(t *testing.T) {
	security := NewServeSecurity(serveWorkflowOptions{
		Listen:      "127.0.0.1:2096",
		AuthToken:   "secret",
		StatePath:   "/state.json",
		ApplyRoot:   "/apply",
		KeyPath:     "/state.key",
		WebBasePath: "secret",
	})
	cfg, err := security.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if cfg.Token != "secret" || cfg.TokenSource != "--auth-token" || cfg.WebBasePath != "/secret/" {
		t.Fatalf("config = %+v", cfg)
	}
}
