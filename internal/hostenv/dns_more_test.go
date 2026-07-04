package hostenv

import (
	"context"
	"errors"
	"net"
	"testing"
)

func TestNetResolverLookupIPReturnsErrorForEmptyHost(t *testing.T) {
	// An empty host should fail locally without performing external DNS lookups.
	var r NetResolver
	_, err := r.LookupIP(context.Background(), "")
	if err == nil {
		t.Fatalf("expected error for empty host")
	}
}

func TestCheckDomainDNSUsesDefaultResolverWhenNil(t *testing.T) {
	// Substitute the package-level default resolver so we can exercise the
	// nil-resolver branch without performing real DNS lookups.
	orig := defaultResolver
	defer func() { defaultResolver = orig }()
	defaultResolver = fakeResolver{ips: []net.IP{net.ParseIP("203.0.113.10")}}

	check, err := CheckDomainDNS(context.Background(), nil, "example.com", net.ParseIP("203.0.113.10"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !check.MatchesPublicIP {
		t.Fatalf("expected public IP to match: %+v", check)
	}
}

func TestCheckDomainDNSReturnsResolverError(t *testing.T) {
	wantErr := errors.New("lookup failed")
	_, err := CheckDomainDNS(context.Background(), fakeResolver{err: wantErr}, "example.com", net.ParseIP("203.0.113.10"))
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected resolver error %v, got %v", wantErr, err)
	}
}

func TestCheckDomainDNSHandlesNilPublicIP(t *testing.T) {
	check, err := CheckDomainDNS(context.Background(), fakeResolver{
		ips: []net.IP{net.ParseIP("203.0.113.10")},
	}, "example.com", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if check.PublicIP != "" {
		t.Fatalf("expected empty PublicIP, got %q", check.PublicIP)
	}
	if check.MatchesPublicIP {
		t.Fatalf("expected no match when public IP is nil: %+v", check)
	}
	if len(check.Warnings) != 0 {
		t.Fatalf("expected no warnings when public IP is nil: %+v", check)
	}
	if len(check.ResolvedIPs) != 1 {
		t.Fatalf("expected 1 resolved IP, got %d: %+v", len(check.ResolvedIPs), check)
	}
}

func TestCheckDomainDNSWarnsNoRecordsWithPublicIP(t *testing.T) {
	check, err := CheckDomainDNS(context.Background(), fakeResolver{ips: []net.IP{}}, "example.com", net.ParseIP("203.0.113.10"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if check.MatchesPublicIP {
		t.Fatalf("expected no match when no records: %+v", check)
	}
	if len(check.Warnings) != 2 {
		t.Fatalf("expected 2 warnings, got %d: %+v", len(check.Warnings), check)
	}
	if check.Warnings[1] != "domain example.com has no A/AAAA records" {
		t.Fatalf("unexpected second warning: %q", check.Warnings[1])
	}
}
