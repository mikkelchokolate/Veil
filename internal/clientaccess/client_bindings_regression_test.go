package clientaccess

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

// #1098: a credential-managed inbound whose normalized credentials are all
// revoked/expired/depleted must never re-advertise the legacy inbound
// fallback link — the link would hand out a credential the server no longer
// accepts (or worse, one the render path still does).
func TestFallbackLinksSuppressedWhenInboundHasClientBindings(t *testing.T) {
	settings := model.Settings{Domain: "vpn.example.com"}
	inbound := model.Inbound{
		Name: "managed", Protocol: "hysteria2", Transport: "udp", Port: 443,
		Enabled: true, Password: "inbound-fallback-pass",
		HasClientBindings: true,
	}
	input := ClientAccessLinkInput{
		Settings:   settings,
		Inbound:    inbound,
		LinkName:   "managed/fallback",
		Credential: ClientCredential{Name: "managed", Username: "managed", Password: "inbound-fallback-pass"},
	}
	if link, ok := hysteria2FallbackClientLink(input); ok {
		t.Fatalf("hysteria2 fallback link advertised for a credential-managed inbound: %+v", link)
	}
	input.Inbound.Protocol = "naiveproxy"
	input.Inbound.Transport = "tcp"
	if link, ok := naiveFallbackClientLink(input); ok {
		t.Fatalf("naive fallback link advertised for a credential-managed inbound: %+v", link)
	}
	input.Inbound.Protocol = "mieru"
	if link, ok := mieruFallbackClientLink(input); ok {
		t.Fatalf("mieru fallback link advertised for a credential-managed inbound: %+v", link)
	}
	// Sanity: once the last binding row is gone the inbound is unmanaged again
	// and the legacy fallback link returns.
	input.Inbound.HasClientBindings = false
	input.Inbound.Protocol = "hysteria2"
	input.Inbound.Transport = "udp"
	if _, ok := hysteria2FallbackClientLink(input); !ok {
		t.Fatal("hysteria2 fallback link missing for an unmanaged inbound")
	}
}
