package diagnostics

import (
	"errors"
	"testing"
)

var errDiagnosticTest = errors.New("lookup failed")

func TestDiagnosticToolsDNSLookupReturnsEmptyAddressesOnError(t *testing.T) {
	old := dnsLookuper
	dnsLookuper = func(host string) ([]string, string, error) { return nil, "", errDiagnosticTest }
	t.Cleanup(func() { dnsLookuper = old })

	result := DiagnosticTools{}.DNSLookup("example.com")
	if result["hostname"] != "example.com" || result["error"] == "" {
		t.Fatalf("unexpected DNS result: %+v", result)
	}
	addresses, ok := result["addresses"].([]string)
	if !ok || len(addresses) != 0 {
		t.Fatalf("expected empty addresses, got %+v", result["addresses"])
	}
}
