package clientaccess

import (
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestBuildClientLinksBuildsProtocolLinksOutsideHTTPAdapter(t *testing.T) {
	response, err := BuildClientLinks(model.Settings{Domain: "vpn.example.com", NaiveUsername: "veil", NaivePassword: "global", Hysteria2Password: "hy"}, []model.Inbound{{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true, Password: "secret"}})
	if err != nil {
		t.Fatalf("BuildClientLinks: %v", err)
	}
	if response.Count != 1 || len(response.Links) != 1 || !strings.HasPrefix(response.Links[0].URI, "naive+https://") {
		t.Fatalf("response = %+v", response)
	}
}

func TestBuildClientLinksSkipsDomainBasedLinksWhenDomainIsUnset(t *testing.T) {
	response, err := BuildClientLinks(model.Settings{Hysteria2Password: "hy"}, []model.Inbound{
		{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true, Password: "secret", NaiveUsername: "veil"},
		{Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 8443, Enabled: true},
		{Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: 2080, Enabled: true, Password: "mieru-secret"},
		{Name: "olc", Protocol: "olcrtc", Transport: "datachannel", Port: 0, Enabled: true, Password: "olc-secret"},
	})
	if err != nil {
		t.Fatalf("BuildClientLinks should not fail without domain: %v", err)
	}
	if response.Count != 1 || len(response.Links) != 1 {
		t.Fatalf("expected only domainless olcRTC link, got %+v", response)
	}
	if response.Links[0].Protocol != "olcrtc" {
		t.Fatalf("unexpected exported link: %+v", response.Links[0])
	}
}

func TestBuildClientLinksUsesClientProfilesWhenPresent(t *testing.T) {
	settings := Settings{
		Domain:            "vpn.example.com",
		NaiveUsername:     "veil",
		NaivePassword:     "global-naive",
		Hysteria2Password: "global-hy2",
	}
	inbounds := []Inbound{
		{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true, Profiles: []ClientProfile{
			{Name: "alice", Username: "alice", Password: "alice-pass", Enabled: true},
			{Name: "bob", Username: "bob", Password: "bob-pass", Enabled: true},
		}},
		{Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true, Profiles: []ClientProfile{
			{Name: "carol", Username: "carol", Password: "carol-pass", Enabled: true},
		}},
	}

	links, err := BuildClientLinks(settings, inbounds)
	if err != nil {
		t.Fatalf("BuildClientLinks: %v", err)
	}

	byName := map[string]ClientLink{}
	for _, link := range links.Links {
		byName[link.Name] = link
	}
	if byName["naive/alice"].URI != "naive+https://alice:alice-pass@vpn.example.com:443" {
		t.Fatalf("naive alice URI = %q", byName["naive/alice"].URI)
	}
	if byName["naive/bob"].URI != "naive+https://bob:bob-pass@vpn.example.com:443" {
		t.Fatalf("naive bob URI = %q", byName["naive/bob"].URI)
	}
	if !strings.HasPrefix(byName["hy2/carol"].URI, "hysteria2://carol:carol-pass@vpn.example.com:443/") {
		t.Fatalf("hy2 carol URI = %q", byName["hy2/carol"].URI)
	}
}

func TestBuildClientLinksUsesPerInboundPasswordAndGlobalFallback(t *testing.T) {
	settings := Settings{
		Domain:            "vpn.example.com",
		NaiveUsername:     "veil",
		NaivePassword:     "global-naive",
		Hysteria2Password: "global-hy2",
	}
	inbounds := []Inbound{
		{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true},
		{Name: "naive-vip", Protocol: "naiveproxy", Transport: "tcp", Port: 8443, Enabled: true, Password: "vip-naive"},
		{Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true},
		{Name: "hy2-vip", Protocol: "hysteria2", Transport: "udp", Port: 8443, Enabled: true, Password: "vip-hy2"},
	}

	links, err := BuildClientLinks(settings, inbounds)
	if err != nil {
		t.Fatalf("BuildClientLinks: %v", err)
	}

	byName := map[string]ClientLink{}
	for _, link := range links.Links {
		byName[link.Name] = link
	}
	if byName["naive"].URI != "naive+https://veil:global-naive@vpn.example.com:443" {
		t.Fatalf("naive fallback URI = %q", byName["naive"].URI)
	}
	if byName["naive-vip"].URI != "naive+https://veil:vip-naive@vpn.example.com:8443" {
		t.Fatalf("naive per-inbound URI = %q", byName["naive-vip"].URI)
	}
	if !strings.HasPrefix(byName["hy2"].URI, "hysteria2://global-hy2@vpn.example.com:443/") {
		t.Fatalf("hy2 fallback URI = %q", byName["hy2"].URI)
	}
	if !strings.HasPrefix(byName["hy2-vip"].URI, "hysteria2://vip-hy2@vpn.example.com:8443/") {
		t.Fatalf("hy2 per-inbound URI = %q", byName["hy2-vip"].URI)
	}
}

func TestOlcrtcClientLinkGeneration(t *testing.T) {
	t.Run("default values", func(t *testing.T) {
		uri := OlcrtcClientURI("", "", "myroom", "mykey", "mymimo")
		expected := "olcrtc://jitsi?datachannel@myroom#mykey$mymimo"
		if uri != expected {
			t.Errorf("expected %q, got %q", expected, uri)
		}
	})

	t.Run("custom values", func(t *testing.T) {
		uri := OlcrtcClientURI("customauth", "customtransport", "myroom", "mykey", "mymimo")
		expected := "olcrtc://customauth?customtransport@myroom#mykey$mymimo"
		if uri != expected {
			t.Errorf("expected %q, got %q", expected, uri)
		}
	})

	t.Run("integration build links", func(t *testing.T) {
		settings := Settings{
			Domain:          "vpn.example.com",
			OlcrtcAuth:      "customauth",
			OlcrtcTransport: "customtransport",
			OlcrtcRoomID:    "myroom",
		}
		inbounds := []Inbound{
			{Name: "olc-fallback", Protocol: "olcrtc", Enabled: true, Password: "fallbackkey"},
			{Name: "olc-profile", Protocol: "olcrtc", Enabled: true, Password: "fallbackkey", Profiles: []ClientProfile{
				{Name: "alice", Username: "mymimo", Password: "mykey", Enabled: true},
			}},
		}

		response, err := BuildClientLinks(settings, inbounds)
		if err != nil {
			t.Fatalf("BuildClientLinks: %v", err)
		}

		byName := map[string]ClientLink{}
		for _, link := range response.Links {
			byName[link.Name] = link
		}

		if byName["olc-fallback"].URI != "olcrtc://customauth?customtransport@myroom#fallbackkey$" {
			t.Errorf("fallback URI = %q", byName["olc-fallback"].URI)
		}

		// olcRTC uses one shared crypto key (the inbound key), not a per-profile
		// secret, so the profile link carries the inbound key and only the
		// profile username as the "mimo" tag.
		if byName["olc-profile/alice"].URI != "olcrtc://customauth?customtransport@myroom#fallbackkey$mymimo" {
			t.Errorf("profile URI = %q", byName["olc-profile/alice"].URI)
		}
	})
}
