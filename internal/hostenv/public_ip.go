package hostenv

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

func DefaultPublicIPEndpoints() []string {
	return []string{
		"https://api.ipify.org",
		"https://ifconfig.me/ip",
		"https://icanhazip.com",
	}
}

// defaultPublicIPEndpointProvider is used by DetectPublicIP when no endpoints
// are supplied. It is a package-level variable so tests can substitute a stub
// without touching the real public endpoints.
var defaultPublicIPEndpointProvider = DefaultPublicIPEndpoints

func ResolvePublicIP(ctx context.Context, value string, client *http.Client, endpoints []string) (net.IP, error) {
	if value == "" {
		return nil, nil
	}
	if value == "auto" {
		detectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		return DetectPublicIP(detectCtx, client, endpoints)
	}
	parsed := net.ParseIP(value)
	if parsed == nil {
		return nil, fmt.Errorf("public IP must be a valid IPv4 or IPv6 address, or auto")
	}
	return parsed, nil
}

// DetectPublicIPForFamily probes the endpoints over a forced address family
// ("tcp4" or "tcp6") so a dual-stack host can learn BOTH public addresses —
// DetectPublicIP returns whichever family the winning connection used, which
// leaves the other family uncovered on the issued IP certificate (#665).
// A nil result with nil error is impossible: the family probe either returns
// a public address of that family or an error.
func DetectPublicIPForFamily(ctx context.Context, endpoints []string, network string) (net.IP, error) {
	if network != "tcp4" && network != "tcp6" {
		return nil, fmt.Errorf("network must be tcp4 or tcp6, got %q", network)
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	transport := &http.Transport{
		DialContext: func(dialCtx context.Context, _, addr string) (net.Conn, error) {
			return dialer.DialContext(dialCtx, network, addr)
		},
	}
	defer transport.CloseIdleConnections()
	ip, err := DetectPublicIP(ctx, &http.Client{Transport: transport, Timeout: 5 * time.Second}, endpoints)
	if err != nil {
		return nil, err
	}
	// Fail closed if an endpoint answers a forced-family connection with the
	// other family's address (lying proxy, NAT64, captive portal): the result
	// feeds certificate SANs, so a mismatch would certify an address the host
	// does not own on that family.
	if (network == "tcp4") != (ip.To4() != nil) {
		return nil, fmt.Errorf("endpoint answered %s probe with wrong-family address %s", network, ip)
	}
	return ip, nil
}

func DetectPublicIP(ctx context.Context, client *http.Client, endpoints []string) (net.IP, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	if len(endpoints) == 0 {
		endpoints = defaultPublicIPEndpointProvider()
	}
	var failures []string
	for _, endpoint := range endpoints {
		ip, err := detectPublicIPFromEndpoint(ctx, client, endpoint)
		if err == nil {
			return ip, nil
		}
		failures = append(failures, fmt.Sprintf("%s: %v", endpoint, err))
	}
	return nil, fmt.Errorf("could not detect public IP: %s", strings.Join(failures, "; "))
}

func detectPublicIPFromEndpoint(ctx context.Context, client *http.Client, endpoint string) (net.IP, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("unexpected status %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 128))
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(strings.TrimSpace(string(body)))
	if ip == nil {
		return nil, fmt.Errorf("response is not an IP address")
	}
	if !isPublicIP(ip) {
		return nil, fmt.Errorf("%s is not a public IP address", ip)
	}
	return ip, nil
}

// cgnatCIDR covers the carrier-grade NAT address space (RFC 6598).
var cgnatCIDR = func() *net.IPNet {
	_, cidr, err := net.ParseCIDR("100.64.0.0/10")
	if err != nil {
		log.Printf("WARNING: failed to parse CGNAT CIDR 100.64.0.0/10: %v — CGNAT check disabled", err)
		return nil
	}
	return cidr
}()

// docCIDRs covers documentation and reserved address ranges that are not
// globally routable (RFC 5737 TEST-NET-1/2/3, RFC 2544 benchmark).
var docCIDRs = func() []*net.IPNet {
	ranges := []string{
		"192.0.2.0/24",    // TEST-NET-1 (RFC 5737)
		"198.51.100.0/24", // TEST-NET-2 (RFC 5737)
		"203.0.113.0/24",  // TEST-NET-3 (RFC 5737)
		"198.18.0.0/15",   // Benchmark (RFC 2544)
	}
	cidrs := make([]*net.IPNet, 0, len(ranges))
	for _, r := range ranges {
		_, cidr, err := net.ParseCIDR(r)
		if err != nil {
			log.Printf("WARNING: failed to parse doc CIDR %s: %v — skipping range", r, err)
			continue
		}
		cidrs = append(cidrs, cidr)
	}
	return cidrs
}()
