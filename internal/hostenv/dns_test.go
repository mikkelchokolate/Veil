package hostenv

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
	if check.MatchesPublicIP {
		t.Fatalf("expected mismatch: %+v", check)
	}
	// The warning must name both the domain and the public IP so operators
	// can tell which side of the drift is wrong (#854).
	want := []string{"domain example.com does not resolve to public IP 203.0.113.10"}
	if len(check.Warnings) != len(want) {
		t.Fatalf("warnings = %v, want %v", check.Warnings, want)
	}
	for i := range want {
		if check.Warnings[i] != want[i] {
			t.Fatalf("warning[%d] = %q, want %q", i, check.Warnings[i], want[i])
		}
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
	if len(check.Warnings) != 1 || check.Warnings[0] != "domain example.com has no A/AAAA records" {
		t.Fatalf("expected exactly the no-records warning, got %+v", check.Warnings)
	}
	if check.MatchesPublicIP {
		t.Fatalf("expected no match when no records resolved: %+v", check)
	}
}
