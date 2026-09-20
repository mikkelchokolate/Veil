package clientaccess

import (
	"net/url"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

// TestNaiveFallbackClientLinkPreservesPasswordBytes is the #331 regression:
// the server rendered strings.TrimSpace(inbound.Password) while the exported
// share URI embedded the raw stored value. The URI must carry the identical
// bytes the server authenticates.
func TestNaiveFallbackClientLinkPreservesPasswordBytes(t *testing.T) {
	links := NewClientAccessProtocolRegistry().BuildLinks(
		Settings{Domain: "vpn.example.com"},
		Inbound{Name: "naive-a", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true, Password: " padded-pass "},
		nil,
	)
	if len(links) != 1 {
		t.Fatalf("links = %+v, want one naive fallback link", links)
	}
	uri, err := url.Parse(strings.TrimPrefix(links[0].URI, "naive+"))
	if err != nil {
		t.Fatalf("parse URI %q: %v", links[0].URI, err)
	}
	password, _ := uri.User.Password()
	if password != " padded-pass " {
		t.Fatalf("exported password = %q, want the stored bytes %q", password, " padded-pass ")
	}
}

// TestHysteria2FallbackClientLinkPreservesPasswordBytes pins the same
// byte-for-byte contract for the Hysteria2 fallback URI.
func TestHysteria2FallbackClientLinkPreservesPasswordBytes(t *testing.T) {
	links := NewClientAccessProtocolRegistry().BuildLinks(
		Settings{Domain: "vpn.example.com"},
		Inbound{Name: "hy2-a", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true, Password: " padded-pass "},
		nil,
	)
	if len(links) != 1 {
		t.Fatalf("links = %+v, want one hysteria2 fallback link", links)
	}
	uri, err := url.Parse(links[0].URI)
	if err != nil {
		t.Fatalf("parse URI %q: %v", links[0].URI, err)
	}
	if uri.User.Username() != " padded-pass " {
		t.Fatalf("exported password = %q, want the stored bytes %q", uri.User.Username(), " padded-pass ")
	}
}

// TestNaiveFallbackClientLinkWhitespacePasswordFallsThrough keeps the
// empty-check contract: a whitespace-only inbound password counts as unset and
// must not shadow the dynamic credential.
func TestNaiveFallbackClientLinkWhitespacePasswordFallsThrough(t *testing.T) {
	links := NewClientAccessProtocolRegistry().BuildLinks(
		Settings{Domain: "vpn.example.com"},
		Inbound{
			Name: "naive-a", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true,
			Password:       "  ",
			ProtocolFields: map[string]any{"naivePassword": "dynamic-secret"},
		},
		nil,
	)
	if len(links) != 1 {
		t.Fatalf("links = %+v, want one naive fallback link", links)
	}
	uri, err := url.Parse(strings.TrimPrefix(links[0].URI, "naive+"))
	if err != nil {
		t.Fatalf("parse URI %q: %v", links[0].URI, err)
	}
	password, _ := uri.User.Password()
	if password != "dynamic-secret" {
		t.Fatalf("exported password = %q, want %q", password, "dynamic-secret")
	}
}

// TestBuildClientCredentialsMatchesServerUserRules is the #334 regression on
// the export side: normalized credentials override legacy profiles on the
// trimmed username (the server renders the same match), stored bytes pass
// through unchanged, and whitespace-only credentials are skipped on both
// sides instead of being advertised but never authenticating.
func TestBuildClientCredentialsMatchesServerUserRules(t *testing.T) {
	merged, err := BuildClientCredentials(Inbound{
		Name:     "naive-a",
		Protocol: "naiveproxy",
		Profiles: []ClientProfile{
			{Name: "alice", Username: " alice ", Password: "legacy-pass", Enabled: true},
			{Name: "carol", Username: "carol", Password: "carol-pass", Enabled: true},
		},
		RuntimeCredentials: []model.RuntimeCredential{
			{Name: "alice", Username: "alice", Password: " padded-cred "},
			{Name: "blank", Username: "  ", Password: "dead"},
			{Name: "blank2", Username: "dave", Password: "   "},
		},
	})
	if err != nil {
		t.Fatalf("BuildClientCredentials: %v", err)
	}
	if len(merged) != 2 {
		t.Fatalf("merged = %+v, want carol profile plus the overriding alice credential", merged)
	}
	for _, credential := range merged {
		switch credential.Username {
		case "carol":
			if credential.Password != "carol-pass" {
				t.Fatalf("carol credential = %+v", credential)
			}
		case "alice":
			if credential.Password != " padded-cred " {
				t.Fatalf("normalized credential = %+v, want stored bytes", credential)
			}
		default:
			t.Fatalf("unexpected credential %+v (whitespace-only credentials must be skipped)", credential)
		}
	}
}
