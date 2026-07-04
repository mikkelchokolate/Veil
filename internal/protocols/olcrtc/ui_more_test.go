package olcrtc

import (
	"errors"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestGenerateRandomHexError(t *testing.T) {
	orig := randRead
	randRead = func([]byte) (int, error) {
		return 0, errors.New("injected rand.Read error")
	}
	t.Cleanup(func() { randRead = orig })

	if _, err := generateRandomHex(64); err == nil {
		t.Fatal("expected error when rand.Read fails")
	}
}

func TestAutofillGenerateRoomError(t *testing.T) {
	orig := cryptoIntn
	cryptoIntn = func(int) (int, error) {
		return 0, errors.New("injected cryptoIntn error")
	}
	t.Cleanup(func() { cryptoIntn = orig })

	p := New()
	if _, err := p.Autofill(model.Inbound{Name: "x", Protocol: "olcrtc"}); err == nil {
		t.Fatal("expected error when GenerateRoom fails")
	}
}

func TestAutofillGenerateKeyError(t *testing.T) {
	orig := randRead
	randRead = func([]byte) (int, error) {
		return 0, errors.New("injected rand.Read error")
	}
	t.Cleanup(func() { randRead = orig })

	p := New()
	if _, err := p.Autofill(model.Inbound{Name: "x", Protocol: "olcrtc"}); err == nil {
		t.Fatal("expected error when generateRandomHex fails")
	}
}
