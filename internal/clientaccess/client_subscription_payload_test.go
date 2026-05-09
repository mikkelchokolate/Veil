package clientaccess

import "testing"

func TestClientSubscriptionPayloadBuildsOneURIPerLineWithTrailingNewline(t *testing.T) {
	payload := NewClientSubscriptionPayload(ClientLinksResponse{Links: []ClientLink{{URI: "one"}, {URI: "two"}}}).Build()
	if payload != "one\ntwo\n" {
		t.Fatalf("payload = %q", payload)
	}
}

func TestClientSubscriptionPayloadBuildsEmptyPayloadWithTrailingNewline(t *testing.T) {
	payload := NewClientSubscriptionPayload(ClientLinksResponse{}).Build()
	if payload != "\n" {
		t.Fatalf("payload = %q", payload)
	}
}
