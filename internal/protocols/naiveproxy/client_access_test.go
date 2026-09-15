package naiveproxy

import (
	"net/url"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestBuildLinksDual(t *testing.T) {
	settings := model.Settings{DefaultInboundPublicPort: 443}
	inbound := model.Inbound{
		Protocol:       "naiveproxy",
		Profiles:       []model.ClientProfile{{Username: "u", Password: "p", Enabled: true}},
		ProtocolFields: map[string]any{"domain": "p.example.com", "transport": "dual"},
	}
	links, err := BuildLinks(settings, inbound)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 2 {
		t.Fatalf("expected 2 links, got %d", len(links))
	}
}

// TestBuildLinksSkipsDisabledProfiles locks in audit #79/#124/#130: disabled
// profiles must not leak into exported client links.
func TestBuildLinksSkipsDisabledProfiles(t *testing.T) {
	settings := model.Settings{DefaultInboundPublicPort: 443}
	inbound := model.Inbound{
		Protocol: "naiveproxy",
		Profiles: []model.ClientProfile{
			{Username: "on", Password: "p1", Enabled: true},
			{Username: "off", Password: "p2", Enabled: false},
		},
		ProtocolFields: map[string]any{"domain": "p.example.com", "transport": "tcp"},
	}
	links, err := BuildLinks(settings, inbound)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 {
		t.Fatalf("expected 1 link (disabled profile must be omitted), got %d: %+v", len(links), links)
	}
	if links[0].URI != "naive+https://on:p1@p.example.com" {
		t.Fatalf("unexpected URI %q", links[0].URI)
	}
}

// TestBuildLinksPercentEncodesUserinfo locks in audit #191 (red-team): a
// username/password containing '@' or ':' must be percent-encoded, otherwise
// the URI parses to a different host or fails outright.
func TestBuildLinksPercentEncodesUserinfo(t *testing.T) {
	settings := model.Settings{DefaultInboundPublicPort: 443}
	inbound := model.Inbound{
		Protocol: "naiveproxy",
		Profiles: []model.ClientProfile{
			{Username: "u@evil.com", Password: "pa:ss/word", Enabled: true},
		},
		ProtocolFields: map[string]any{"domain": "p.example.com", "transport": "tcp"},
	}
	links, err := BuildLinks(settings, inbound)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}
	if got, want := links[0].URI, "naive+https://u%40evil.com:pa%3Ass%2Fword@p.example.com"; got != want {
		t.Fatalf("URI = %q, want %q", got, want)
	}
	parsed, err := url.Parse(links[0].URI)
	if err != nil {
		t.Fatalf("URI must parse: %v", err)
	}
	if parsed.Host != "p.example.com" {
		t.Fatalf("URI host = %q, want p.example.com (userinfo must not redirect the host)", parsed.Host)
	}
}

func TestBuildLinksOmitsFallbackWhenAllProfilesDisabled(t *testing.T) {
	settings := model.Settings{Domain: "vpn.example.com", NaiveUsername: "veil", NaivePassword: "global"}
	inbound := model.Inbound{
		Name:           "naive",
		Protocol:       "naiveproxy",
		Enabled:        true,
		Profiles:       []model.ClientProfile{{Name: "alice", Username: "alice", Password: "alice-pass", Enabled: false}},
		ProtocolFields: map[string]any{"domain": "vpn.example.com", "transport": "tcp"},
	}
	links, err := BuildLinks(settings, inbound)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 0 {
		t.Fatalf("all-disabled profiles must not revive fallback URI, got %+v", links)
	}
}

func TestBuildLinksEmitsNaivePlusHTTPSAndBracketsIPv6(t *testing.T) {
	settings := model.Settings{Domain: "2001:db8::20", DefaultInboundPublicPort: 443}
	inbound := model.Inbound{
		Protocol:       "naiveproxy",
		Profiles:       []model.ClientProfile{{Username: "alice", Password: "pass", Enabled: true}},
		ProtocolFields: map[string]any{"transport": "tcp"},
	}
	links, err := BuildLinks(settings, inbound)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}
	if links[0].URI != "naive+https://alice:pass@[2001:db8::20]" {
		t.Fatalf("URI = %q", links[0].URI)
	}
	parsed, err := url.Parse(links[0].URI)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme != "naive+https" {
		t.Fatalf("scheme = %q", parsed.Scheme)
	}
	if parsed.Hostname() != "2001:db8::20" {
		t.Fatalf("hostname = %q", parsed.Hostname())
	}
}
