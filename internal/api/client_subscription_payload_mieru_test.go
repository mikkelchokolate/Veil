package api

import "testing"

func TestClientSubscriptionPayloadSkipsConfigOnlyMieruLinks(t *testing.T) {
	payload := NewClientSubscriptionPayload(ClientLinksResponse{Links: []ClientLink{
		{Name: "mieru", Protocol: "mieru", Config: "{}"},
		{Name: "hy2", Protocol: "hysteria2", URI: "hysteria2://secret@example.com:443"},
	}}).Build()
	if payload != "hysteria2://secret@example.com:443\n" {
		t.Fatalf("payload = %q", payload)
	}
}
