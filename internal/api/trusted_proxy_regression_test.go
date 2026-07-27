package api

import (
	"net/http/httptest"
	"testing"
)

func TestAuditCanonicalClientAddressIgnoresUntrustedForwardedFor(t *testing.T) {
	req := httptest.NewRequest("GET", "http://panel/api/status", nil)
	req.RemoteAddr = "198.51.100.20:54321"
	req.Header.Set("X-Forwarded-For", "203.0.113.99")
	if got := clientIP(req); got != "198.51.100.20" {
		t.Fatalf("audit client address trusted forged X-Forwarded-For: got %q", got)
	}
}
