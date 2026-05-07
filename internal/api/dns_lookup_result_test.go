package api

import (
	"errors"
	"testing"
)

func TestDNSLookupResultBuildsStableResponseShape(t *testing.T) {
	result := NewDNSLookupResult("example.com", nil, "example.net.", errors.New("boom")).Map()
	if result["hostname"] != "example.com" {
		t.Fatalf("hostname = %+v", result)
	}
	if addrs, ok := result["addresses"].([]string); !ok || len(addrs) != 0 {
		t.Fatalf("addresses = %#v", result["addresses"])
	}
	if result["cname"] != "example.net." || result["error"] != "boom" {
		t.Fatalf("result = %+v", result)
	}
}
