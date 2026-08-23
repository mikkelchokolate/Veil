package olcrtc

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
		if got := ProviderSupportsAutoRoom(provider); got != want {
			t.Errorf("ProviderSupportsAutoRoom(%q) = %v, want %v", provider, got, want)
		}
	}
}

func TestGenerateOlcrtcRoomJitsiProducesURL(t *testing.T) {
	room, err := GenerateRoom("jitsi")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(room, "https://") {
		t.Fatalf("jitsi room is not a URL: %q", room)
	}
	// Two generations must differ (random room name).
	other, _ := GenerateRoom("jitsi")
	if room == other {
		t.Fatalf("rooms should be random, got %q twice", room)
	}
}

func TestGenerateOlcrtcRoomLooksNaturalWithNoPanelMarker(t *testing.T) {
	// Generate many rooms: none may leak a tool/panel marker, each must be a
	// valid room path segment (letters/digits/dashes), and across the sample we
	// should observe more than one format (words, hex, dashes, …) so there is no
	// single fingerprintable pattern.
	shapes := map[string]bool{}
	for i := 0; i < 200; i++ {
		room, err := GenerateRoom("jitsi")
		if err != nil {
			t.Fatal(err)
		}
		lower := strings.ToLower(room)
		for _, marker := range []string{"veil", "panel", "olcrtc", "generated"} {
			if strings.Contains(lower, marker) {
				t.Fatalf("room URL %q leaks marker %q", room, marker)
			}
		}
		name := strings.TrimPrefix(room, JitsiRoomBase)
		if name == room || name == "" {
			t.Fatalf("unexpected room URL shape: %q", room)
		}
		for _, c := range name {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-') {
				t.Fatalf("room name has an invalid path char in %q", name)
			}
		}
		switch {
		case strings.Contains(name, "-"):
			shapes["dash"] = true
		case name[0] >= 'a' && name[0] <= 'z':
			shapes["lowerOrHexOrAlnum"] = true
		default:
			shapes["camel"] = true
		}
	}
	if len(shapes) < 2 {
		t.Fatalf("expected varied room formats, only saw: %v", shapes)
	}
}

func TestGenerateOlcrtcRoomRefusesManualProviders(t *testing.T) {
	for _, provider := range []string{"telemost", "wbstream", "unknown"} {
		if _, err := GenerateRoom(provider); err == nil {
			t.Errorf("GenerateOlcrtcRoom(%q) should error (manual room required)", provider)
		}
	}
}
