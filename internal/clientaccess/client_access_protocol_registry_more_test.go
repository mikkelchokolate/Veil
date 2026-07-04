package clientaccess

import "testing"

func TestProtocolStringBranches(t *testing.T) {
	cases := []struct {
		name     string
		m        map[string]any
		key      string
		fallback string
		want     string
	}{
		{"nil map", nil, "k", "fb", "fb"},
		{"missing key", map[string]any{"other": "v"}, "k", "fb", "fb"},
		{"non-string value", map[string]any{"k": 123}, "k", "fb", "fb"},
		{"string value trimmed", map[string]any{"k": "  value  "}, "k", "fb", "value"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := protocolString(tc.m, tc.key, tc.fallback); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestProtocolBoolBranches(t *testing.T) {
	cases := []struct {
		name     string
		m        map[string]any
		key      string
		fallback bool
		want     bool
	}{
		{"nil map", nil, "k", true, true},
		{"missing key", map[string]any{"other": true}, "k", false, false},
		{"non-bool value", map[string]any{"k": "yes"}, "k", false, false},
		{"bool true", map[string]any{"k": true}, "k", false, true},
		{"bool false", map[string]any{"k": false}, "k", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := protocolBool(tc.m, tc.key, tc.fallback); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBuildAllLinksSkipsDisabledAndUnknownProtocols(t *testing.T) {
	registry := NewClientAccessProtocolRegistry()
	links, err := registry.BuildAllLinks(Settings{Domain: "vpn.example.com"}, []Inbound{
		{Name: "disabled", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: false},
		{Name: "unknown", Protocol: "unknown", Transport: "tcp", Port: 443, Enabled: true},
		{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true, Password: "secret", NaiveUsername: "u"},
	})
	if err != nil {
		t.Fatalf("BuildAllLinks: %v", err)
	}
	if len(links) != 1 || links[0].Name != "naive" {
		t.Fatalf("links = %+v", links)
	}
}

func TestBuildAllLinksReturnsAggregateError(t *testing.T) {
	registry := NewClientAccessProtocolRegistry()
	_, err := registry.BuildAllLinks(Settings{Domain: "vpn.example.com"}, []Inbound{
		{Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Profiles: []ClientProfile{{Name: "alice", Enabled: true}}},
	})
	if err == nil {
		t.Fatal("expected error from mieru aggregate credentials")
	}
}

func TestBuildAllLinksReturnsPerInboundCredentialError(t *testing.T) {
	registry := NewClientAccessProtocolRegistry()
	_, err := registry.BuildAllLinks(Settings{Domain: "vpn.example.com"}, []Inbound{
		{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true, Profiles: []ClientProfile{{Name: "alice", Enabled: true}}},
	})
	if err == nil {
		t.Fatal("expected error from naive BuildClientAccess")
	}
}

func TestNaiveProfileLinkRequiresDomain(t *testing.T) {
	link, ok := naiveProfileClientLink(ClientAccessLinkInput{
		Settings:   Settings{},
		Inbound:    Inbound{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true},
		LinkName:   "naive/alice",
		Credential: ClientCredential{Name: "alice", Username: "alice", Password: "pass"},
	})
	if ok {
		t.Fatalf("expected no link without domain, got %+v", link)
	}
}

func TestHysteria2ProfileLinkRequiresDomain(t *testing.T) {
	link, ok := hysteria2ProfileClientLink(ClientAccessLinkInput{
		Settings:   Settings{},
		Inbound:    Inbound{Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true},
		LinkName:   "hy2/alice",
		Credential: ClientCredential{Name: "alice", Username: "alice", Password: "pass"},
	})
	if ok {
		t.Fatalf("expected no link without domain, got %+v", link)
	}
}

func TestHysteria2InsecureBranches(t *testing.T) {
	cases := []struct {
		name string
		in   ClientAccessLinkInput
		want bool
	}{
		{"inbound flag", ClientAccessLinkInput{Inbound: Inbound{Hysteria2Insecure: true}}, true},
		{"inbound protocol field", ClientAccessLinkInput{Inbound: Inbound{ProtocolFields: map[string]any{"hysteria2Insecure": true}}}, true},
		{"settings flag", ClientAccessLinkInput{Settings: Settings{Hysteria2Insecure: true}}, true},
		{"settings protocol field", ClientAccessLinkInput{Settings: Settings{ProtocolFields: map[string]any{"hysteria2Insecure": true}}}, true},
		{"none false", ClientAccessLinkInput{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hysteria2Insecure(tc.in); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMieruClientConfigLinkRequiresDomain(t *testing.T) {
	link, ok := mieruClientConfigLink(ClientAccessLinkInput{
		Settings:   Settings{},
		Inbound:    Inbound{Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Password: "pass"},
		LinkName:   "mieru",
		Credential: ClientCredential{Name: "mieru", Username: "mieru", Password: "pass"},
	})
	if ok {
		t.Fatalf("expected no link without domain, got %+v", link)
	}
}
