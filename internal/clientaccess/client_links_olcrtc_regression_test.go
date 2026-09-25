package clientaccess

import (
	"net/url"
	"strings"
	"testing"
)

// TestOlcrtcClientURIEscapesRoomIDReservedChars covers #1002: a room ID
// containing URI-reserved characters corrupted the share URI — '#'
// displaced the key fragment, '?'/'$' split the grammar, whitespace
// produced invalid bytes. The fragment must now carry exactly key$mimo and
// the URI must parse cleanly.
func TestOlcrtcClientURIEscapesRoomIDReservedChars(t *testing.T) {
	key := strings.Repeat("a", 64)

	uri := OlcrtcClientURI("jitsi", "datachannel", "meet.example.org/room#injected", key, "alice")
	parsed, err := url.Parse(uri)
	if err != nil {
		t.Fatalf("rendered URI does not parse: %q: %v", uri, err)
	}
	// The fragment slot the client reads must hold the full key$mimo pair —
	// the room's own '#' must never reach it raw.
	if parsed.Fragment != key+"$alice" {
		t.Fatalf("fragment = %q, want key$mimo payload %q", parsed.Fragment, key+"$alice")
	}
	if !strings.Contains(uri, "%23injected") {
		t.Fatalf("room '#' was not percent-encoded: %q", uri)
	}
	if !strings.HasPrefix(uri, "olcrtc://jitsi?datachannel@meet.example.org/room") {
		t.Fatalf("room shape changed: %q", uri)
	}

	for _, room := range []string{"room?x=1", "room$key", "room with space", "room@evil"} {
		uri := OlcrtcClientURI("jitsi", "datachannel", room, key, "")
		parsed, err := url.Parse(uri)
		if err != nil {
			t.Fatalf("room %q produced unparseable URI %q: %v", room, uri, err)
		}
		if parsed.Fragment != key+"$" {
			t.Fatalf("room %q corrupted fragment: %q", room, parsed.Fragment)
		}
	}
}

// TestOlcrtcClientURIPreservesJitsiRoomShape locks the legitimate room
// format: scheme://host/room must keep its ':' and '/' readable.
func TestOlcrtcClientURIPreservesJitsiRoomShape(t *testing.T) {
	uri := OlcrtcClientURI("jitsi", "datachannel", "https://meet.example.org/veil-abcd1234", "k", "")
	if !strings.Contains(uri, "@https://meet.example.org/veil-abcd1234#") {
		t.Fatalf("jitsi room URL was mangled: %q", uri)
	}
}

// TestOlcrtcClientURIEscapesMimo covers the '$'-suffixed trailing field.
func TestOlcrtcClientURIEscapesMimo(t *testing.T) {
	uri := OlcrtcClientURI("telemost", "vp8channel", "room-1", "k", "we#ird")
	if !strings.HasSuffix(uri, "#k$we%23ird") {
		t.Fatalf("mimo not escaped: %q", uri)
	}
}
