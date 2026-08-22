package clientaccess

import (
	"strings"
	"testing"
)

func TestOlcrtcRegistryExportUsesEffectiveProtocolFieldKey(t *testing.T) {
	const key = "abababababababababababababababababababababababababababababababab"
	inbound := Inbound{
		Name: "olc", Protocol: "olcrtc", Transport: "udp", Enabled: true,
		ProtocolFields: map[string]any{
			"password":        key,
			"olcrtcAuth":      "jitsi",
			"olcrtcTransport": "datachannel",
			"olcrtcRoomID":    "https://meet.example.test/room",
		},
	}
	effective := clientLinkEffectiveInbounds([]Inbound{inbound})
	if len(effective) != 1 || effective[0].Password != key {
		t.Fatalf("effective password = %q, want protocolFields password", effective[0].Password)
	}
	if inbound.Password != "" {
		t.Fatal("effective link normalization mutated desired state")
	}
	links, err := NewClientAccessProtocolRegistry().BuildAllLinks(Settings{}, effective)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || !strings.Contains(links[0].URI, "#"+key+"$") {
		t.Fatalf("olcRTC registry URI = %v, want effective crypto key", links)
	}
}
