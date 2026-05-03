package installer

import (
	"context"
	"net"
	"testing"
)

type fakeResolver struct {
	ips []net.IP
	err error
}

func (f fakeResolver) LookupIP(ctx context.Context, host string) ([]net.IP, error) {
	return f.ips, f.err
}

func TestCheckDomainDNSMatchesPublicIP(t *testing.T) {
	check, err := CheckDomainDNS(context.Background(), fakeResolver{ips: []net.IP{net.ParseIP("203.0.113.10")}}, "example.com", net.ParseIP("203.0.113.10"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !check.MatchesPublicIP || len(check.Warnings) != 0 {
		t.Fatalf("unexpected check: %+v", check)
	}
}

func TestCheckDomainDNSWarnsWhenPublicIPDiffers(t *testing.T) {
	check, err := CheckDomainDNS(context.Background(), fakeResolver{ips: []net.IP{net.ParseIP("203.0.113.11")}}, "example.com", net.ParseIP("203.0.113.10"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if check.MatchesPublicIP || len(check.Warnings) == 0 {
		t.Fatalf("expected warning: %+v", check)
	}
}

func TestCheckDomainDNSRejectsInvalidDomain(t *testing.T) {
	_, err := CheckDomainDNS(context.Background(), fakeResolver{}, "localhost", net.ParseIP("203.0.113.10"))
	if err == nil {
		t.Fatalf("expected invalid domain error")
	}
}

func TestCheckDomainDNSSkipsNilIPsAndWarnsOnEmptyResults(t *testing.T) {
	// Nil IPs mixed with valid IPs: nil entries should be skipped.
	check, err := CheckDomainDNS(context.Background(), fakeResolver{
		ips: []net.IP{nil, net.ParseIP("203.0.113.10"), nil},
	}, "example.com", net.ParseIP("203.0.113.10"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(check.ResolvedIPs) != 1 {
		t.Fatalf("expected 1 resolved IP after skipping nils, got %d: %+v", len(check.ResolvedIPs), check)
	}
	if check.ResolvedIPs[0] != "203.0.113.10" {
		t.Fatalf("expected resolved IP 203.0.113.10, got %s", check.ResolvedIPs[0])
	}
	if !check.MatchesPublicIP {
		t.Fatalf("expected IP to match public IP: %+v", check)
	}

	// Empty IP list: warning about no records should be generated.
	check, err = CheckDomainDNS(context.Background(), fakeResolver{
		ips: []net.IP{},
	}, "example.com", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(check.Warnings) == 0 {
		t.Fatalf("expected warning about no records, got none: %+v", check)
	}
	if check.MatchesPublicIP {
		t.Fatalf("expected no match when no records resolved: %+v", check)
	}
}
