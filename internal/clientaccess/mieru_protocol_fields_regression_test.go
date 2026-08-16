package clientaccess

import (
	"strings"
	"testing"
)

func TestMieruAggregateExportUsesEffectiveProtocolFieldPassword(t *testing.T) {
	const password = "dynamic-secret"
	inbound := Inbound{
		Name: "mieru-one", Protocol: "mieru", Transport: "tcp", Port: 23456, Enabled: true,
		ProtocolFields: map[string]any{"password": password},
	}
	effective := clientLinkEffectiveInbounds([]Inbound{inbound})
	if len(effective) != 1 || effective[0].Password != password {
		t.Fatalf("effective password = %q, want protocolFields password", effective[0].Password)
	}
	links, err := NewMieruClientAccessAggregator().Build(Settings{Domain: "vpn.example.test"}, effective)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 {
		t.Fatalf("links = %d, want 1", len(links))
	}
	if !strings.Contains(links[0].URI, password) || !strings.Contains(links[0].Config, password) {
		t.Fatalf("mieru export lost effective password: uri=%q config=%q", links[0].URI, links[0].Config)
	}
}
