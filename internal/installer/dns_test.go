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
