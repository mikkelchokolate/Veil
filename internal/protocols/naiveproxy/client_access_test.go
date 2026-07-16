package naiveproxy

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestBuildLinksDual(t *testing.T) {
	settings := model.Settings{DefaultInboundPublicPort: 443}
	inbound := model.Inbound{
		Protocol: "naiveproxy",
		Profiles: []model.ClientProfile{{Username: "u", Password: "p"}},
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
