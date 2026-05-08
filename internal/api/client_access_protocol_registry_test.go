package api

import (
	"strings"
	"testing"
)

func TestClientAccessProtocolRegistryBuildsProtocolSpecificLinks(t *testing.T) {
	registry := NewClientAccessProtocolRegistry()
	settings := Settings{Domain: "vpn.example.com", NaiveUsername: "veil", NaivePassword: "naive-secret", Hysteria2Password: "hy2-secret"}

	naive := registry.BuildLinks(settings, Inbound{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true}, []ClientCredential{{Name: "alice", Username: "alice", Password: "alice-pass"}})
	if len(naive) != 1 || naive[0].Name != "naive/alice" || naive[0].URI != "naive+https://alice:alice-pass@vpn.example.com:443" {
		t.Fatalf("naive links = %+v", naive)
	}

	hy2 := registry.BuildLinks(settings, Inbound{Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true}, nil)
	if len(hy2) != 1 || hy2[0].Name != "hy2" || !strings.HasPrefix(hy2[0].URI, "hysteria2://hy2-secret@vpn.example.com:443/") {
		t.Fatalf("hysteria2 fallback links = %+v", hy2)
	}

	mieru := registry.BuildLinks(settings, Inbound{Name: "mieru", Protocol: "mieru", Transport: "udp", Port: 9443, Enabled: true, Password: "mieru-secret"}, nil)
	if len(mieru) != 1 || mieru[0].Name != "mieru" || mieru[0].URI != "" || !strings.Contains(mieru[0].Config, `"protocol": "UDP"`) || !strings.Contains(mieru[0].Config, `"password": "mieru-secret"`) {
		t.Fatalf("mieru fallback links = %+v", mieru)
	}
}

func TestClientAccessProtocolRegistrySkipsUnknownProtocols(t *testing.T) {
	links := NewClientAccessProtocolRegistry().BuildLinks(Settings{Domain: "vpn.example.com"}, Inbound{Name: "unknown", Protocol: "unknown", Transport: "tcp", Port: 443, Enabled: true}, []ClientCredential{{Name: "alice", Username: "alice", Password: "pass"}})
	if len(links) != 0 {
		t.Fatalf("unknown protocol links = %+v", links)
	}
}
