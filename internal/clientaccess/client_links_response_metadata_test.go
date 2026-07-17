package clientaccess

import "testing"

func TestClientLinksResponseMetadataBuildsStableSubscriptionFields(t *testing.T) {
	response := NewClientLinksResponseMetadata(Settings{Domain: "example.com"}).Build()
	if response.SchemaVersion != "v1" || response.Domain != "example.com" {
		t.Fatalf("response = %+v", response)
	}
	if response.SubscriptionURL != "/api/client-links/subscription" || response.Base64SubscriptionURL == "" || response.RawSubscriptionURL == "" {
		t.Fatalf("subscription urls = %+v", response)
	}
	if len(response.SubscriptionFormats) != 2 || response.SubscriptionFormats[0] != "base64" || response.SubscriptionFormats[1] != "raw" {
		t.Fatalf("formats = %+v", response.SubscriptionFormats)
	}
}

func TestClientLinksResponseMetadataDomainMirrorsSettings(t *testing.T) {
	// ClientLinksResponse.Domain intentionally reflects the global settings
	// domain, not a per-inbound domain. Individual links carry their own
	// resolved domains, so this top-level field remains stable metadata.
	response := NewClientLinksResponseMetadata(Settings{}).Build()
	if response.Domain != "" {
		t.Fatalf("empty settings should yield empty response.Domain, got %q", response.Domain)
	}
	response = NewClientLinksResponseMetadata(Settings{Domain: "vpn.example.com"}).Build()
	if response.Domain != "vpn.example.com" {
		t.Fatalf("response.Domain = %q, want vpn.example.com", response.Domain)
	}
}
