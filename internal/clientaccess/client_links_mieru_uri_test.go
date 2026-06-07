package clientaccess

import (
	"net/url"
	"strings"
	"testing"
)

// TestMieruClientURIFormat verifies the exact mierus:// output for known inputs.
// The format is validated against `mieru explain config`, which decodes this
// URI back into a valid client profile (host, user, port, protocol).
func TestMieruClientURIFormat(t *testing.T) {
	got := MieruClientURI("45.157.233.54", 3453, "client_ic09f", "CJViw6Sm0o0v", "veil", "udp")
	want := "mierus://client_ic09f:CJViw6Sm0o0v@45.157.233.54?port=3453&profile=veil&protocol=UDP"
	if got != want {
		t.Fatalf("mieru URI =\n  %q\nwant\n  %q", got, want)
	}
}

func TestMieruClientURIProtocolNormalization(t *testing.T) {
	for _, tc := range []struct {
		transport string
		want      string
	}{
		{"udp", "protocol=UDP"},
		{"UDP", "protocol=UDP"},
		{"tcp", "protocol=TCP"},
		{"", "protocol=TCP"},      // unset defaults to TCP
		{"weird", "protocol=TCP"}, // unknown defaults to TCP
	} {
		if got := MieruClientURI("h", 80, "u", "p", "x", tc.transport); !strings.Contains(got, tc.want) {
			t.Fatalf("transport %q -> %q, want %q", tc.transport, got, tc.want)
		}
	}
}

func TestMieruClientURIEscapesCredentials(t *testing.T) {
	got := MieruClientURI("host", 80, "user name", "p@ss/word?x", "prof ile", "udp")
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("URI must be parseable, got %q: %v", got, err)
	}
	if parsed.Scheme != "mierus" {
		t.Fatalf("scheme = %q", parsed.Scheme)
	}
	if parsed.User.Username() != "user name" {
		t.Fatalf("username round-trip = %q", parsed.User.Username())
	}
	if pw, _ := parsed.User.Password(); pw != "p@ss/word?x" {
		t.Fatalf("password round-trip = %q", pw)
	}
	if parsed.Query().Get("profile") != "prof ile" {
		t.Fatalf("profile round-trip = %q", parsed.Query().Get("profile"))
	}
}

// TestBuildClientLinksMieruHasImportableURI is the end-to-end check: a mieru
// inbound must yield a client link that has BOTH a mierus:// URI (for QR and
// subscriptions) and a JSON config, pointing clients at the right server.
func TestBuildClientLinksMieruHasImportableURI(t *testing.T) {
	settings := Settings{Domain: "45.157.233.54", Mode: "server"}
	inbounds := []Inbound{
		{Name: "m1", Protocol: "mieru", Transport: "udp", Port: 3453, Enabled: true, Profiles: []ClientProfile{
			{Name: "alice", Username: "alice", Password: "alicepw", Enabled: true},
		}},
	}
	resp, err := BuildClientLinks(settings, inbounds)
	if err != nil {
		t.Fatalf("BuildClientLinks: %v", err)
	}
	var mieru *ClientLink
	for i := range resp.Links {
		if resp.Links[i].Protocol == "mieru" {
			mieru = &resp.Links[i]
		}
	}
	if mieru == nil {
		t.Fatalf("no mieru link generated: %+v", resp.Links)
	}
	for _, want := range []string{"mierus://", "alice:alicepw@45.157.233.54", "port=3453", "protocol=UDP"} {
		if !strings.Contains(mieru.URI, want) {
			t.Fatalf("mieru URI %q missing %q", mieru.URI, want)
		}
	}
	if mieru.Config == "" {
		t.Fatal("mieru link must also carry a JSON config")
	}
}
