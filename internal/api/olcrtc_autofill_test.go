package api

import (
	"reflect"
	"strings"
	"testing"
)

func TestAutofillOlcrtcInboundProvisionsRoomAndKey(t *testing.T) {
	in, err := autofillOlcrtcInbound(Inbound{Name: "olc", Protocol: "olcrtc", Transport: "udp", Port: 6523})
	if err != nil {
		t.Fatal(err)
	}
	if in.OlcrtcAuth != "jitsi" {
		t.Fatalf("auth = %q, want jitsi", in.OlcrtcAuth)
	}
	if in.OlcrtcTransport != "datachannel" {
		t.Fatalf("transport = %q, want datachannel", in.OlcrtcTransport)
	}
	if !strings.HasPrefix(in.OlcrtcRoomID, "https://") || !strings.Contains(in.OlcrtcRoomID, "/veil-") {
		t.Fatalf("room id not auto-generated: %q", in.OlcrtcRoomID)
	}
	if !isOlcrtcKey(in.Password) {
		t.Fatalf("password is not a 64-hex olcrtc key: %q", in.Password)
	}
}

func TestAutofillOlcrtcInboundPreservesExistingAndIgnoresOthers(t *testing.T) {
	// Existing fields are preserved (so re-saving does not rotate the room/key
	// and break already-issued clients).
	existing := Inbound{
		Protocol: "olcrtc", OlcrtcAuth: "telemost", OlcrtcTransport: "vp8channel",
		OlcrtcRoomID: "https://meet.example/room1",
		Password:     "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff",
	}
	got, err := autofillOlcrtcInbound(existing)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, existing) {
		t.Fatalf("existing olcrtc fields were mutated: %+v", got)
	}

	// Non-olcrtc inbounds are untouched.
	mieru := Inbound{Name: "m", Protocol: "mieru", Password: "short"}
	out, err := autofillOlcrtcInbound(mieru)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out, mieru) {
		t.Fatalf("non-olcrtc inbound mutated: %+v", out)
	}
}
