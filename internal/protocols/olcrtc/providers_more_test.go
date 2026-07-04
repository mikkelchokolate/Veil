package olcrtc

import (
	"errors"
	"testing"
)

func TestGenerateRoomRandomError(t *testing.T) {
	orig := cryptoIntn
	cryptoIntn = func(int) (int, error) {
		return 0, errors.New("injected cryptoIntn error")
	}
	t.Cleanup(func() { cryptoIntn = orig })

	if _, err := GenerateRoom("jitsi"); err == nil {
		t.Fatal("expected error when randomRoomName fails")
	}
}
