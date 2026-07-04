package diagnostics

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

func TestDNSLookupResultMapSuccessWithAddresses(t *testing.T) {
	result := NewDNSLookupResult("example.com", []string{"203.0.113.1", "2001:db8::1"}, "", nil).Map()
	if result["hostname"] != "example.com" {
		t.Fatalf("hostname = %+v", result)
	}
	if result["error"] != nil {
		t.Fatalf("unexpected error: %+v", result["error"])
	}
	if _, ok := result["cname"]; ok {
		t.Fatalf("cname should be omitted when empty, got %+v", result["cname"])
	}
	addrs, ok := result["addresses"].([]string)
	if !ok || len(addrs) != 2 || addrs[0] != "203.0.113.1" || addrs[1] != "2001:db8::1" {
		t.Fatalf("addresses = %+v", result["addresses"])
	}
}

func TestDNSLookupResultMapSuccessWithCNAMEAndAddresses(t *testing.T) {
	result := NewDNSLookupResult("example.com", []string{"203.0.113.1"}, "canonical.example.com.", nil).Map()
	if result["hostname"] != "example.com" {
		t.Fatalf("hostname = %+v", result)
	}
	if result["cname"] != "canonical.example.com." {
		t.Fatalf("cname = %+v", result["cname"])
	}
	if result["error"] != nil {
		t.Fatalf("unexpected error: %+v", result["error"])
	}
	addrs, ok := result["addresses"].([]string)
	if !ok || len(addrs) != 1 || addrs[0] != "203.0.113.1" {
		t.Fatalf("addresses = %+v", result["addresses"])
	}
}

func TestDNSLookupResultMapErrorWithoutCNAME(t *testing.T) {
	result := NewDNSLookupResult("bad.example.com", nil, "", errors.New("nxdomain")).Map()
	if result["hostname"] != "bad.example.com" {
		t.Fatalf("hostname = %+v", result)
	}
	if result["error"] != "nxdomain" {
		t.Fatalf("error = %+v", result["error"])
	}
	if _, ok := result["cname"]; ok {
		t.Fatalf("cname should be omitted when empty")
	}
	addrs, ok := result["addresses"].([]string)
	if !ok || len(addrs) != 0 {
		t.Fatalf("addresses = %+v", result["addresses"])
	}
}

func TestDNSLookupResultMapEmptyAddressesNotNil(t *testing.T) {
	result := NewDNSLookupResult("example.com", []string{}, "", nil).Map()
	addrs, ok := result["addresses"].([]string)
	if !ok || len(addrs) != 0 {
		t.Fatalf("addresses = %+v", result["addresses"])
	}
	if result["error"] != nil {
		t.Fatalf("unexpected error: %+v", result["error"])
	}
}
