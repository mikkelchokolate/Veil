package api

import "testing"

func TestClientLinksResponseMetadataBuildsStableSubscriptionFields(t *testing.T) {
	response := NewClientLinksResponseMetadata(Settings{Domain: "example.com", Stack: "both"}).Build()
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
