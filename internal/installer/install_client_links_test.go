package installer

import "testing"

func TestInstallClientLinksBuildsNaiveAndHysteriaLinks(t *testing.T) {
	links := NewInstallClientLinks()
	naive := links.Naive("veil", "secret pass", "example.com", 443)
	hysteria := links.Hysteria2("hy secret", "example.com", 8443)
	if naive != "naive+https://veil:secret%20pass@example.com:443" {
		t.Fatalf("naive = %q", naive)
	}
	if hysteria != "hysteria2://hy%20secret@example.com:8443?insecure=0" {
		t.Fatalf("hysteria = %q", hysteria)
	}
}
