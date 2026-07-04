package clientaccess

import (
	"strings"
	"testing"
)

func TestBuildClientLinksPropagatesBuildAllLinksError(t *testing.T) {
	_, err := BuildClientLinks(Settings{Domain: "vpn.example.com"}, []Inbound{
		{Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Profiles: []ClientProfile{{Name: "alice", Enabled: true}}},
	})
	if err == nil {
		t.Fatal("expected error for invalid mieru credentials")
	}
}

func TestHysteria2ClientURIIncludesInsecure(t *testing.T) {
	uri := Hysteria2ClientURI("vpn.example.com", 443, "pass", "name", true)
	if !strings.Contains(uri, "insecure=1") {
		t.Fatalf("URI missing insecure: %q", uri)
	}
}

func TestHysteria2UserPassClientURIIncludesInsecure(t *testing.T) {
	uri := Hysteria2UserPassClientURI("vpn.example.com", 443, "user", "pass", "name", true)
	if !strings.Contains(uri, "insecure=1") {
		t.Fatalf("URI missing insecure: %q", uri)
	}
}
