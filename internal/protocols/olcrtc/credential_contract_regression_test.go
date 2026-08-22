package olcrtc

import (
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/clientaccess"
	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestEffectiveKeyProtocolFieldsWinsEverywhere(t *testing.T) {
	p := New()
	flatKey := strings.Repeat("a", 64)
	dynamicKey := strings.Repeat("b", 64)
	room := "https://meet.example.org/credential-contract"
	inbound := model.Inbound{
		Name:      "credential-contract",
		Protocol:  "olcrtc",
		Transport: "udp",
		Password:  flatKey,
		ProtocolFields: map[string]any{
			"password":        dynamicKey,
			"olcrtcAuth":      "jitsi",
			"olcrtcTransport": "datachannel",
			"olcrtcRoomID":    room,
		},
	}

	issues := p.ValidateInbound(model.Settings{}, inbound)
	for _, issue := range issues {
		if issue.Severity == "error" {
			t.Fatalf("effective dynamic key state should validate, got %+v", issues)
		}
	}
	if !p.HasCredential(model.Settings{}, inbound) {
		t.Fatal("HasCredential must see protocolFields password")
	}

	arts, ok, err := p.RenderConfig(generatedconfig.ProtocolRenderInput{
		Settings: model.Settings{},
		Paths:    generatedconfig.NewPaths("/etc/veil"),
		Inbounds: []model.Inbound{inbound},
	})
	if err != nil {
		t.Fatalf("RenderConfig error: %v", err)
	}
	if !ok || len(arts) != 1 {
		t.Fatalf("expected one rendered artifact, ok=%v len=%d", ok, len(arts))
	}
	if !strings.Contains(arts[0].Body, "key: "+dynamicKey) {
		t.Fatalf("renderer did not use effective key; body:\n%s", arts[0].Body)
	}
	if strings.Contains(arts[0].Body, "key: "+flatKey) {
		t.Fatalf("renderer used stale flat key; body:\n%s", arts[0].Body)
	}

	links, err := p.BuildLinks(model.Settings{}, inbound)
	if err != nil {
		t.Fatalf("BuildLinks error: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("expected one client link, got %d", len(links))
	}
	wantURI := clientaccess.OlcrtcClientURI("jitsi", "datachannel", room, dynamicKey, "")
	if links[0].URI != wantURI {
		t.Fatalf("client export used a different key: got %q want %q", links[0].URI, wantURI)
	}
}
