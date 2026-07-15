package naiveproxy

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

// TestBuildLinks_NaiveTCP443OmitsPort verifies the happy-path client export URL
// for a TCP naiveproxy inbound on the default public port.
func TestBuildLinks_NaiveTCP443OmitsPort(t *testing.T) {
	settings := model.Settings{
		DefaultInboundPublicPort: 443,
		DefaultAcmeEmail:         "admin@hy.flow2go.ru",
	}
	inbound := model.Inbound{
		Name:     "test",
		Protocol: "naiveproxy",
		Enabled:  true,
		Profiles: []model.ClientProfile{
			{Name: "default", Username: "user", Password: "pass", Enabled: true},
		},
		ProtocolFields: map[string]any{
			"domain":    "hy.flow2go.ru",
			"transport": "tcp",
		},
	}

	links, err := BuildLinks(settings, inbound)
	if err != nil {
		t.Fatalf("BuildLinks error: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}

	link := links[0]
	if link.Transport != "tcp" {
		t.Errorf("link.Transport = %q, want tcp", link.Transport)
	}
	if link.Port != 443 {
		t.Errorf("link.Port = %d, want 443", link.Port)
	}
	want := "https://user:pass@hy.flow2go.ru"
	if link.URI != want {
		t.Errorf("link.URI = %q, want %q", link.URI, want)
	}
}
