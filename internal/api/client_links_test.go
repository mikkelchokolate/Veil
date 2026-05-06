package api

import (
	"strings"
	"testing"
)

func TestBuildClientLinksUsesPerInboundPasswordAndGlobalFallback(t *testing.T) {
	settings := Settings{
		Domain:            "vpn.example.com",
		Stack:             "both",
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
	if byName["naive"].URI != "https://veil:global-naive@vpn.example.com:443" {
		t.Fatalf("naive fallback URI = %q", byName["naive"].URI)
	}
	if byName["naive-vip"].URI != "https://veil:vip-naive@vpn.example.com:8443" {
		t.Fatalf("naive per-inbound URI = %q", byName["naive-vip"].URI)
	}
	if !strings.HasPrefix(byName["hy2"].URI, "hysteria2://global-hy2@vpn.example.com:443/") {
		t.Fatalf("hy2 fallback URI = %q", byName["hy2"].URI)
	}
	if !strings.HasPrefix(byName["hy2-vip"].URI, "hysteria2://vip-hy2@vpn.example.com:8443/") {
		t.Fatalf("hy2 per-inbound URI = %q", byName["hy2-vip"].URI)
	}
}
