package protocols

import (
	"strings"
	"testing"
)

func TestOlcrtcProviderAutoRoomCapability(t *testing.T) {
	cases := map[string]bool{
		"jitsi":    true,
		"telemost": false,
		"wbstream": false,
		"unknown":  false,
	}
	for provider, want := range cases {
		if got := OlcrtcProviderSupportsAutoRoom(provider); got != want {
			t.Errorf("OlcrtcProviderSupportsAutoRoom(%q) = %v, want %v", provider, got, want)
		}
	}
}

func TestGenerateOlcrtcRoomJitsiProducesURL(t *testing.T) {
	room, err := GenerateOlcrtcRoom("jitsi")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(room, "https://") {
		t.Fatalf("jitsi room is not a URL: %q", room)
	}
	// Two generations must differ (random room name).
	other, _ := GenerateOlcrtcRoom("jitsi")
	if room == other {
		t.Fatalf("rooms should be random, got %q twice", room)
	}
}

func TestGenerateOlcrtcRoomRefusesManualProviders(t *testing.T) {
	for _, provider := range []string{"telemost", "wbstream", "unknown"} {
		if _, err := GenerateOlcrtcRoom(provider); err == nil {
			t.Errorf("GenerateOlcrtcRoom(%q) should error (manual room required)", provider)
		}
	}
}
