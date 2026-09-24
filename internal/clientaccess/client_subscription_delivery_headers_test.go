package clientaccess

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
	// These two headers are the security contract for subscription delivery:
	// no-store keeps credentials out of caches and nosniff stops content-type
	// confusion — lock the exact values, not just presence (#892).
	if got := header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if got := header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
}
