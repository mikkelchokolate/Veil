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

func TestGenerateOlcrtcRoomLooksNaturalWithNoPanelMarker(t *testing.T) {
	// Generate several rooms and assert none leak a tool/panel marker and each
	// looks like a normal Jitsi room name (CamelCase words after the base URL).
	for i := 0; i < 20; i++ {
		room, err := GenerateOlcrtcRoom("jitsi")
		if err != nil {
			t.Fatal(err)
		}
		lower := strings.ToLower(room)
		for _, marker := range []string{"veil", "panel", "olcrtc", "room-", "generated"} {
			if strings.Contains(lower, marker) {
				t.Fatalf("room URL %q leaks marker %q", room, marker)
			}
		}
		name := strings.TrimPrefix(room, "https://meet.small-dm.ru/")
		if name == room || name == "" {
			t.Fatalf("unexpected room URL shape: %q", room)
		}
		// Natural name: starts with an uppercase letter, only letters, no digits
		// or dashes (unlike the old "veil-<hex>" pattern).
		if name[0] < 'A' || name[0] > 'Z' {
			t.Fatalf("room name should start uppercase like a Jitsi room: %q", name)
		}
		for _, c := range name {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
				t.Fatalf("room name should be letters only (natural Jitsi style), got %q", name)
			}
		}
	}
}

func TestGenerateOlcrtcRoomRefusesManualProviders(t *testing.T) {
	for _, provider := range []string{"telemost", "wbstream", "unknown"} {
		if _, err := GenerateOlcrtcRoom(provider); err == nil {
			t.Errorf("GenerateOlcrtcRoom(%q) should error (manual room required)", provider)
		}
	}
}
