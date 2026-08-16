package olcrtc

import (
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestRenderOlcrtcRejectsMissingPersistedKey(t *testing.T) {
	if _, err := renderOlcrtc(model.Settings{}, model.Inbound{Name: "x"}); err == nil || !strings.Contains(err.Error(), "encryption key") {
		t.Fatalf("missing key error = %v, want explicit persisted-key failure", err)
	}
}

func TestRenderConfigPropagatesMissingKeyError(t *testing.T) {
	p := New()
	_, _, err := p.RenderConfig(generatedconfig.ProtocolRenderInput{
		Settings: model.Settings{},
		Paths:    generatedconfig.NewPaths("/etc/veil"),
		Inbounds: []model.Inbound{{Name: "x", Protocol: "olcrtc"}},
	})
	if err == nil || !strings.Contains(err.Error(), "encryption key") {
		t.Fatalf("RenderConfig error = %v, want missing persisted key", err)
	}
}

func TestRenderOlcrtcIsDeterministicWithPersistedKey(t *testing.T) {
	inbound := model.Inbound{
		Name: "x", Protocol: "olcrtc", Password: strings.Repeat("ab", 32),
		OlcrtcAuth: "jitsi", OlcrtcTransport: "datachannel", OlcrtcRoomID: "https://meet.example.test/room",
	}
	first, err := renderOlcrtc(model.Settings{}, inbound)
	if err != nil {
		t.Fatal(err)
	}
	second, err := renderOlcrtc(model.Settings{}, inbound)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("same persisted olcRTC state rendered different server configs")
	}
	if !strings.Contains(first, strings.Repeat("ab", 32)) {
		t.Fatal("persisted encryption key missing from rendered config")
	}
}
