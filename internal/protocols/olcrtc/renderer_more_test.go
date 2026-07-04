package olcrtc

import (
	"errors"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestRenderOlcrtcRandReadError(t *testing.T) {
	orig := randRead
	randRead = func([]byte) (int, error) {
		return 0, errors.New("injected rand.Read error")
	}
	t.Cleanup(func() { randRead = orig })

	if _, err := renderOlcrtc(model.Settings{}, model.Inbound{Name: "x"}); err == nil {
		t.Fatal("expected error when rand.Read fails")
	}
}

func TestRenderConfigRenderError(t *testing.T) {
	orig := randRead
	randRead = func([]byte) (int, error) {
		return 0, errors.New("injected rand.Read error")
	}
	t.Cleanup(func() { randRead = orig })

	p := New()
	_, _, err := p.RenderConfig(generatedconfig.ProtocolRenderInput{
		Settings: model.Settings{},
		Paths:    generatedconfig.NewPaths("/etc/veil"),
		Inbounds: []model.Inbound{{Name: "x", Protocol: "olcrtc"}},
	})
	if err == nil {
		t.Fatal("expected error when renderOlcrtc fails")
	}
}
