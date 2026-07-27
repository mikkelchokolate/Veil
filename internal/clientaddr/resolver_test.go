package clientaddr

import (
	"net/http/httptest"
	"testing"
)

func TestTrustedProxyChainIsParsedFromTrustedSide(t *testing.T) {
	resolver, err := New([]string{"10.0.0.0/8", "192.0.2.0/24"})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "http://panel/api/status", nil)
	req.RemoteAddr = "10.0.0.9:443"
	req.Header.Set("X-Forwarded-For", "203.0.113.250, 198.51.100.25, 192.0.2.4")
	got, err := resolver.Resolve(req)
	if err != nil {
		t.Fatal(err)
	}
	if got != "198.51.100.25" {
		t.Fatalf("resolved=%q want rightmost untrusted client hop", got)
	}
}

func TestTrustedProxyRejectsMalformedForwardedEntry(t *testing.T) {
	resolver, _ := New([]string{"10.0.0.0/8"})
	req := httptest.NewRequest("GET", "http://panel/api/status", nil)
	req.RemoteAddr = "10.0.0.9:443"
	req.Header.Set("X-Forwarded-For", "198.51.100.25, malformed")
	if _, err := resolver.Resolve(req); err == nil {
		t.Fatal("expected malformed trusted proxy chain rejection")
	}
}

func TestUntrustedPeerHeaderIsIgnoredEvenWhenMalformed(t *testing.T) {
	resolver, _ := New([]string{"10.0.0.0/8"})
	req := httptest.NewRequest("GET", "http://panel/api/status", nil)
	req.RemoteAddr = "198.51.100.7:1234"
	req.Header.Set("X-Forwarded-For", "malformed")
	got, err := resolver.Resolve(req)
	if err != nil {
		t.Fatal(err)
	}
	if got != "198.51.100.7" {
		t.Fatalf("resolved=%q", got)
	}
}
