package api

import (
	"net/http/httptest"
	"testing"
)

func TestClientLinkDeliveryHeadersApplyNoStoreAndNosniff(t *testing.T) {
	w := httptest.NewRecorder()
	NewClientLinkDeliveryHeaders().Apply(w.Header())
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("headers = %+v", w.Header())
	}
}
