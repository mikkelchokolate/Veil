package clientaccess

import (
	"net"
	"net/url"
	"strings"
	"testing"
)

func TestNaiveShareURIUsesGUIScheme(t *testing.T) {
	uri := NaiveShareURI("vpn.example.com", 443, "alice", "alice-pass", "https", 443)
	if uri != "naive+https://alice:alice-pass@vpn.example.com" {
		t.Fatalf("URI = %q", uri)
	}
	if strings.HasPrefix(uri, "https://") {
		t.Fatalf("share URI must not use the naive binary --proxy= scheme: %q", uri)
	}
	quic := NaiveShareURI("vpn.example.com", 443, "alice", "alice-pass", "quic", 443)
	if quic != "naive+quic://alice:alice-pass@vpn.example.com" {
		t.Fatalf("quic URI = %q", quic)
	}
}

func TestNaiveProfileAndFallbackLinksUseNaivePlusHTTPS(t *testing.T) {
	profile, ok := naiveProfileClientLink(ClientAccessLinkInput{
		Settings:   Settings{Domain: "vpn.example.com"},
		Inbound:    Inbound{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true},
		LinkName:   "naive/alice",
		Credential: ClientCredential{Name: "alice", Username: "alice", Password: "alice-pass"},
	})
	if !ok {
		t.Fatal("expected profile link")
	}
	if !strings.HasPrefix(profile.URI, "naive+https://") {
		t.Fatalf("profile URI = %q, want naive+https://", profile.URI)
	}

	fallback, ok := naiveFallbackClientLink(ClientAccessLinkInput{
		Settings: Settings{Domain: "vpn.example.com", NaiveUsername: "veil", NaivePassword: "secret"},
		Inbound:  Inbound{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 8443, Enabled: true},
		LinkName: "naive",
	})
	if !ok {
		t.Fatal("expected fallback link")
	}
	if fallback.URI != "naive+https://veil:secret@vpn.example.com:8443" {
		t.Fatalf("fallback URI = %q", fallback.URI)
	}
}

func TestShareURIsBracketIPv6Literals(t *testing.T) {
	const ip = "2001:db8::20"
	naiveDefault := NaiveShareURI(ip, 443, "alice", "pass", "https", 443)
	if naiveDefault != "naive+https://alice:pass@[2001:db8::20]" {
		t.Fatalf("naive :443 URI = %q", naiveDefault)
	}
	naivePort := NaiveShareURI(ip, 8443, "alice", "pass", "https", 443)
	if naivePort != "naive+https://alice:pass@[2001:db8::20]:8443" {
		t.Fatalf("naive :8443 URI = %q", naivePort)
	}

	hy2 := Hysteria2UserPassClientURI(ip, 443, "alice", "pass", "hy2/alice", false)
	host, port := mustURIHostPort(t, hy2)
	if host != ip || port != "443" {
		t.Fatalf("hysteria2 host:port = %s:%s, want %s:443 (uri %q)", host, port, ip, hy2)
	}
	if !strings.Contains(hy2, "@[2001:db8::20]:443/") {
		t.Fatalf("hysteria2 URI missing bracketed host: %q", hy2)
	}

	mieru := MieruClientURI(ip, 9443, "alice", "pass", "mieru/alice", "tcp")
	host, port = mustURIHostPort(t, mieru)
	if host != ip {
		t.Fatalf("mieru host = %q, want %s (uri %q)", host, ip, mieru)
	}
	if port != "" {
		t.Fatalf("mieru authority must not carry a port, got %q from %q", port, mieru)
	}
	if !strings.Contains(mieru, "@[2001:db8::20]?") {
		t.Fatalf("mieru URI missing bracketed host: %q", mieru)
	}
}

func TestShareURIsKeepIPv4AndDNSUnbracketed(t *testing.T) {
	naive := NaiveShareURI("vpn.example.com", 443, "alice", "pass", "https", 443)
	if strings.Contains(naive, "[") {
		t.Fatalf("DNS name must stay unbracketed: %q", naive)
	}
	hy2 := Hysteria2ClientURI("203.0.113.10", 443, "pass", "veil", false)
	if strings.Contains(hy2, "[") {
		t.Fatalf("IPv4 must stay unbracketed: %q", hy2)
	}
	if !strings.Contains(hy2, "@203.0.113.10:443/") {
		t.Fatalf("IPv4 hysteria2 URI = %q", hy2)
	}
}

func TestRegistryIPv6ShareURIsRoundTrip(t *testing.T) {
	settings := Settings{Domain: "2001:db8::20"}
	registry := NewClientAccessProtocolRegistry()

	naive := registry.BuildLinks(settings, Inbound{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true}, []ClientCredential{{Name: "alice", Username: "alice", Password: "pass"}})
	if len(naive) != 1 {
		t.Fatalf("naive links = %+v", naive)
	}
	host, _ := mustURIHostPort(t, naive[0].URI)
	if host != "2001:db8::20" {
		t.Fatalf("naive host = %q from %q", host, naive[0].URI)
	}

	hy2 := registry.BuildLinks(settings, Inbound{Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 9443, Enabled: true}, []ClientCredential{{Name: "alice", Username: "alice", Password: "pass"}})
	if len(hy2) != 1 {
		t.Fatalf("hy2 links = %+v", hy2)
	}
	host, port := mustURIHostPort(t, hy2[0].URI)
	if host != "2001:db8::20" || port != "9443" {
		t.Fatalf("hy2 host:port = %s:%s from %q", host, port, hy2[0].URI)
	}

	mieru := registry.BuildLinks(settings, Inbound{Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: 2080, Enabled: true, Password: "pass"}, nil)
	if len(mieru) != 1 {
		t.Fatalf("mieru links = %+v", mieru)
	}
	host, _ = mustURIHostPort(t, mieru[0].URI)
	if host != "2001:db8::20" {
		t.Fatalf("mieru host = %q from %q", host, mieru[0].URI)
	}
	parsed, err := url.Parse(mieru[0].URI)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Query().Get("port") != "2080" {
		t.Fatalf("mieru query port = %q from %q", parsed.Query().Get("port"), mieru[0].URI)
	}
}

func mustURIHostPort(t *testing.T, uri string) (string, string) {
	t.Helper()
	parsed, err := url.Parse(uri)
	if err != nil {
		t.Fatalf("parse %q: %v", uri, err)
	}
	host, port, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		return parsed.Hostname(), ""
	}
	return host, port
}
