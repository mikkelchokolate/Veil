package api

import (
	"net/http"
	"testing"
)

func TestClientSubscriptionDeliveryHeadersAppliesCacheContentAndDisposition(t *testing.T) {
	header := http.Header{}
	NewClientSubscriptionDeliveryHeaders(ClientSubscription{ContentType: "text/plain", Filename: "veil.txt"}).Apply(header)
	if header.Get("Content-Type") != "text/plain" {
		t.Fatalf("content-type = %q", header.Get("Content-Type"))
	}
	if header.Get("Content-Disposition") != `attachment; filename="veil.txt"` {
		t.Fatalf("content-disposition = %q", header.Get("Content-Disposition"))
	}
	if header.Get("Cache-Control") == "" || header.Get("X-Content-Type-Options") == "" {
		t.Fatalf("missing base client link headers: %+v", header)
	}
}
