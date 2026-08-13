package naiveproxy

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestBuildLinksDual(t *testing.T) {
	settings := model.Settings{DefaultInboundPublicPort: 443}
	inbound := model.Inbound{
		Protocol:       "naiveproxy",
		Profiles:       []model.ClientProfile{{Username: "u", Password: "p", Enabled: true}},
		ProtocolFields: map[string]any{"domain": "p.example.com", "transport": "dual"},
	}
	links, err := BuildLinks(settings, inbound)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 2 {
		t.Fatalf("expected 2 links, got %d", len(links))
	}
}

// TestBuildLinksSkipsDisabledProfiles locks in audit #79/#124/#130: disabled
// profiles must not leak into exported client links.
func TestBuildLinksSkipsDisabledProfiles(t *testing.T) {
	settings := model.Settings{DefaultInboundPublicPort: 443}
	inbound := model.Inbound{
		Protocol: "naiveproxy",
		Profiles: []model.ClientProfile{
			{Username: "on", Password: "p1", Enabled: true},
			{Username: "off", Password: "p2", Enabled: false},
		},
		ProtocolFields: map[string]any{"domain": "p.example.com", "transport": "tcp"},
	}
	links, err := BuildLinks(settings, inbound)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 {
		t.Fatalf("expected 1 link (disabled profile must be omitted), got %d: %+v", len(links), links)
	}
	if links[0].URI != "https://on:p1@p.example.com" {
		t.Fatalf("unexpected URI %q", links[0].URI)
	}
}
