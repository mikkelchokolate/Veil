package clientaccess

import (
	"strings"
	"testing"
)

func TestBuildClientLinksUsesDynamicMieruFallbackPassword(t *testing.T) {
	const password = "dynamic-mieru-secret"
	response, err := BuildClientLinks(Settings{Domain: "vpn.example.com"}, []Inbound{{
		Name: "mieru", Protocol: "mieru", Transport: "udp", Port: 9443, Enabled: true,
		ProtocolFields: map[string]any{"password": password},
	}})
	if err != nil {
		t.Fatalf("BuildClientLinks: %v", err)
	}
	if response.Count != 1 || len(response.Links) != 1 {
		t.Fatalf("dynamic Mieru fallback was not exported: %+v", response)
	}
	if !strings.Contains(response.Links[0].Config, `"password": "`+password+`"`) || !strings.Contains(response.Links[0].URI, password) {
		t.Fatalf("dynamic Mieru password missing from client export: %+v", response.Links[0])
	}
}

func TestRegistryDirectMieruFallbackUsesDynamicPassword(t *testing.T) {
	const password = "dynamic-direct-secret"
	links := NewClientAccessProtocolRegistry().BuildLinks(Settings{Domain: "vpn.example.com"}, Inbound{
		Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: 9443, Enabled: true,
		ProtocolFields: map[string]any{"password": password},
	}, nil)
	if len(links) != 1 || !strings.Contains(links[0].Config, `"password": "`+password+`"`) || !strings.Contains(links[0].URI, password) {
		t.Fatalf("direct registry Mieru fallback = %+v", links)
	}
}

func TestBuildClientLinksUsesDynamicOlcRTCKey(t *testing.T) {
	key := strings.Repeat("ab", 32)
	room := "https://meet.example.org/veil-dynamic-key"
	response, err := BuildClientLinks(Settings{}, []Inbound{{
		Name: "olcrtc", Protocol: "olcrtc", Transport: "udp", Port: 12000, Enabled: true,
		ProtocolFields: map[string]any{
			"password":        key,
			"olcrtcAuth":      "jitsi",
			"olcrtcTransport": "datachannel",
			"olcrtcRoomID":    room,
		},
	}})
	if err != nil {
		t.Fatalf("BuildClientLinks: %v", err)
	}
	if response.Count != 1 || len(response.Links) != 1 {
		t.Fatalf("dynamic olcRTC key was not exported: %+v", response)
	}
	want := "olcrtc://jitsi?datachannel@" + room + "#" + key + "$"
	if response.Links[0].URI != want {
		t.Fatalf("olcRTC dynamic-key URI = %q, want %q", response.Links[0].URI, want)
	}
}
