package renderer

import (
	"strings"
	"testing"
)

func TestRenderHysteria2RecommendedConfig(t *testing.T) {
	cfg, err := RenderHysteria2(Hysteria2Config{
		ListenPort: 443,
		Domain: "example.com",
		Password: "secret-pass",
		MasqueradeURL: "https://www.bing.com/",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{
		"listen: :443",
		"type: password",
		"password: secret-pass",
		"type: proxy",
		"url: https://www.bing.com/",
		"rewriteHost: true",
	} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("rendered config missing %q:\n%s", want, cfg)
		}
	}
}

func TestRenderHysteria2RequiresListenPort(t *testing.T) {
	_, err := RenderHysteria2(Hysteria2Config{
		Password:      "secret",
		MasqueradeURL: "https://www.bing.com/",
	})
	if err == nil {
		t.Fatal("expected error for missing listen port, got nil")
	}
	if !strings.Contains(err.Error(), "listen port is required") {
		t.Fatalf("expected 'listen port is required', got: %v", err)
	}
}

func TestRenderHysteria2RequiresPassword(t *testing.T) {
	_, err := RenderHysteria2(Hysteria2Config{
		ListenPort:    443,
		MasqueradeURL: "https://www.bing.com/",
	})
	if err == nil {
		t.Fatal("expected error for missing password, got nil")
	}
	if !strings.Contains(err.Error(), "password is required") {
		t.Fatalf("expected 'password is required', got: %v", err)
	}
}

func TestRenderHysteria2DefaultsMasqueradeURL(t *testing.T) {
	cfg, err := RenderHysteria2(Hysteria2Config{
		ListenPort: 443,
		Password:   "secret",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(cfg, "url: https://www.bing.com/") {
		t.Fatalf("expected default masquerade URL, got:\n%s", cfg)
	}
}

func TestRenderHysteria2RejectsZeroListenPort(t *testing.T) {
	_, err := RenderHysteria2(Hysteria2Config{
		ListenPort: 0,
		Password:   "secret",
	})
	if err == nil {
		t.Fatal("expected error for zero listen port, got nil")
	}
	if !strings.Contains(err.Error(), "listen port is required") {
		t.Fatalf("expected 'listen port is required', got: %v", err)
	}
}

func TestRenderHysteria2RejectsNegativeListenPort(t *testing.T) {
	_, err := RenderHysteria2(Hysteria2Config{
		ListenPort: -1,
		Password:   "secret",
	})
	if err == nil {
		t.Fatal("expected error for negative listen port, got nil")
	}
	if !strings.Contains(err.Error(), "listen port is required") {
		t.Fatalf("expected 'listen port is required', got: %v", err)
	}
}
