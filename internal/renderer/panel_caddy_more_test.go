package renderer

import (
	"strings"
	"testing"
)

func TestRenderPanelCaddyfileValidation(t *testing.T) {
	tests := []struct {
		name      string
		cfg       PanelCaddyConfig
		wantError string
	}{
		{
			name:      "missing domain",
			cfg:       PanelCaddyConfig{PanelPort: 2096, WebBasePath: "/panel/"},
			wantError: "domain is required",
		},
		{
			name:      "missing panel port",
			cfg:       PanelCaddyConfig{Domain: "example.com", WebBasePath: "/panel/"},
			wantError: "panel port is required",
		},
		{
			name:      "missing web base path",
			cfg:       PanelCaddyConfig{Domain: "example.com", PanelPort: 2096},
			wantError: "web base path is required",
		},
		{
			name:      "root web base path",
			cfg:       PanelCaddyConfig{Domain: "example.com", PanelPort: 2096, WebBasePath: "/"},
			wantError: "web base path is required",
		},
		{
			name:      "directive injection in web base path",
			cfg:       PanelCaddyConfig{Domain: "example.com", PanelPort: 2096, WebBasePath: "panel\nrespond hacked"},
			wantError: "web base path:",
		},
		{
			name:      "query in web base path",
			cfg:       PanelCaddyConfig{Domain: "example.com", PanelPort: 2096, WebBasePath: "panel?debug"},
			wantError: "web base path:",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := RenderPanelCaddyfile(tt.cfg)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("expected %q, got: %v", tt.wantError, err)
			}
		})
	}
}

func TestRenderPanelCaddyfileAddsTrailingSlash(t *testing.T) {
	body, err := RenderPanelCaddyfile(PanelCaddyConfig{
		Domain:      "example.com",
		PanelPort:   2096,
		WebBasePath: "/panel-secret",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(body, "handle /panel-secret {") || !strings.Contains(body, "redir * /panel-secret/ 308") {
		t.Fatalf("expected exact-path redirect:\n%s", body)
	}
	if !strings.Contains(body, "handle /panel-secret/* {") {
		t.Fatalf("expected wildcard handler with trailing slash:\n%s", body)
	}
}
