package renderer

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestRenderMieruClientDefaultsToValidSocks5Port is the regression guard for
// audit series #51/#101: the delivered client config must be importable by the
// upstream mieru client, which rejects socks5Port < 1 ("socks5 port number 0
// is invalid"). A zero Socks5Port must be replaced with a deterministic valid
// port in [1024, 65535].
func TestRenderMieruClientDefaultsToValidSocks5Port(t *testing.T) {
	body, err := RenderMieruClient(MieruClientConfig{
		ProfileName:  "mieru/alice",
		DomainName:   "vpn.example.com",
		PortBindings: []MieruPortBinding{{Port: 443, Protocol: "tcp"}},
		User:         MieruUser{Name: "alice", Password: "alice-pass"},
		// Socks5Port intentionally left at 0 (the production caller does).
	})
	if err != nil {
		t.Fatalf("RenderMieruClient: %v", err)
	}
	var decoded struct {
		Socks5Port int `json:"socks5Port"`
		RPCPort    int `json:"rpcPort"`
	}
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, body)
	}
	if decoded.Socks5Port < 1 || decoded.Socks5Port > 65535 {
		t.Fatalf("socks5Port = %d, want in [1, 65535]", decoded.Socks5Port)
	}
	if decoded.RPCPort < 0 {
		t.Fatalf("rpcPort = %d, want >= 0", decoded.RPCPort)
	}
}

// TestRenderMieruClientSocks5PortDeterministic ensures the derived port is
// stable across renders for the same identity (config diffs stay empty).
func TestRenderMieruClientSocks5PortDeterministic(t *testing.T) {
	render := func() int {
		body, err := RenderMieruClient(MieruClientConfig{
			ProfileName:  "mieru/bob",
			DomainName:   "vpn.example.com",
			PortBindings: []MieruPortBinding{{Port: 8443, Protocol: "udp"}},
			User:         MieruUser{Name: "bob", Password: "bob-pass"},
		})
		if err != nil {
			t.Fatalf("RenderMieruClient: %v", err)
		}
		var decoded struct {
			Socks5Port int `json:"socks5Port"`
		}
		if err := json.Unmarshal([]byte(body), &decoded); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		return decoded.Socks5Port
	}
	first := render()
	second := render()
	if first != second {
		t.Fatalf("socks5Port not deterministic: %d vs %d", first, second)
	}
	if first < 1024 || first > 65535 {
		t.Fatalf("socks5Port = %d, want in [1024, 65535]", first)
	}
}

// TestMieruDefaultSocks5PortRange covers the derivation helper boundaries.
func TestMieruDefaultSocks5PortRange(t *testing.T) {
	seen := map[int]bool{}
	for _, identity := range []struct{ profile, user, endpoint string }{
		{"mieru/a", "a", "vpn.example.com"},
		{"mieru/b", "b", "vpn.example.com"},
		{"m2", "gen-fallback", "10.0.0.1"},
		{"x/y/z", "with:colon", "2001:db8::1"},
		{"", "", ""},
	} {
		port := mieruDefaultSocks5Port(identity.profile, identity.user, identity.endpoint)
		if port < 1024 || port > 65535 {
			t.Fatalf("port %d out of range for %+v", port, identity)
		}
		seen[port] = true
	}
	if len(seen) < 3 {
		t.Fatalf("port derivation lacks spread: %v", seen)
	}
}

// TestMieruClientConfigExplicitPortsRespected ensures caller-provided ports
// are never overridden.
func TestMieruClientConfigExplicitPortsRespected(t *testing.T) {
	body, err := RenderMieruClient(MieruClientConfig{
		ProfileName:   "mieru/alice",
		DomainName:    "vpn.example.com",
		PortBindings:  []MieruPortBinding{{Port: 443, Protocol: "tcp"}},
		User:          MieruUser{Name: "alice", Password: "alice-pass"},
		Socks5Port:    1080,
		HTTPProxyPort: 8080,
		RPCPort:       9000,
	})
	if err != nil {
		t.Fatalf("RenderMieruClient: %v", err)
	}
	for _, want := range []string{`"socks5Port": 1080`, `"httpProxyPort": 8080`, `"rpcPort": 9000`} {
		if !strings.Contains(body, want) {
			t.Fatalf("config missing %s:\n%s", want, body)
		}
	}
}
