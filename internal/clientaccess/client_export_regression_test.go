package clientaccess

import (
	"strings"
	"testing"
)

func TestCloneSettingsDetachesProtocolFields(t *testing.T) {
	original := Settings{
		Domain:             "vpn.example.com",
		Hysteria2Insecure:  true,
		ProtocolFields:     map[string]any{"hysteria2Insecure": true, "nested": map[string]any{"k": "v"}},
		FirewallManagement: boolPtr(false),
	}
	cloned := CloneSettings(original)
	cloned.ProtocolFields["hysteria2Insecure"] = false
	cloned.ProtocolFields["nested"].(map[string]any)["k"] = "mutated"
	*cloned.FirewallManagement = true
	if original.ProtocolFields["hysteria2Insecure"] != true {
		t.Fatal("CloneSettings aliased ProtocolFields")
	}
	if original.ProtocolFields["nested"].(map[string]any)["k"] != "v" {
		t.Fatal("CloneSettings aliased nested ProtocolFields")
	}
	if *original.FirewallManagement {
		t.Fatal("CloneSettings aliased FirewallManagement")
	}
}

func TestMaterializeInboundCopiesLegacyProtocolFields(t *testing.T) {
	inbound := Inbound{
		Name:              "hy2",
		Protocol:          "hysteria2",
		Hysteria2Insecure: true,
		Hysteria2Password: "secret",
		OlcrtcAuth:        "jitsi",
	}
	got := MaterializeInbound(inbound)
	if got.ProtocolFields["hysteria2Insecure"] != true {
		t.Fatalf("expected hysteria2Insecure materialized, got %#v", got.ProtocolFields)
	}
	if got.ProtocolFields["hysteria2Password"] != "secret" {
		t.Fatalf("expected hysteria2Password materialized, got %#v", got.ProtocolFields)
	}
	if inbound.ProtocolFields != nil {
		t.Fatal("MaterializeInbound mutated the original inbound")
	}
	got.ProtocolFields["hysteria2Insecure"] = false
	if MaterializeInbound(inbound).ProtocolFields["hysteria2Insecure"] != true {
		t.Fatal("materialized fields were aliased")
	}
}

func TestCloneSettingsPreservesGlobalHysteria2InsecureForLinkBuilder(t *testing.T) {
	settings := CloneSettings(Settings{Domain: "vpn.example.com", Hysteria2Insecure: true})
	inbound := MaterializeInbound(Inbound{Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true})
	links := NewClientAccessProtocolRegistry().BuildLinks(settings, inbound, []ClientCredential{{Name: "alice", Username: "alice", Password: "pw"}})
	if len(links) != 1 || !strings.Contains(links[0].URI, "insecure=1") {
		t.Fatalf("expected insecure=1 from global settings, got %#v", links)
	}
	domainOnly := Settings{Domain: settings.Domain}
	dropped := NewClientAccessProtocolRegistry().BuildLinks(domainOnly, inbound, []ClientCredential{{Name: "alice", Username: "alice", Password: "pw"}})
	if len(dropped) != 1 || strings.Contains(dropped[0].URI, "insecure=1") {
		t.Fatalf("domain-only settings should drop the opt-in, got %#v", dropped)
	}
}

func boolPtr(v bool) *bool { return &v }
