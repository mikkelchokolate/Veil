package installer

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

func DetectPublicIP(ctx context.Context, client *http.Client, endpoints []string) (net.IP, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	if len(endpoints) == 0 {
		endpoints = DefaultPublicIPEndpoints()
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

func isPublicIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	// Reject unspecified, loopback, private, link-local, multicast, and CGNAT.
	if ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() {
		return false
	}
	if cgnatCIDR != nil && cgnatCIDR.Contains(ip) {
		return false
	}
	// Reject documentation and reserved test ranges.
	for _, cidr := range docCIDRs {
		if cidr != nil && cidr.Contains(ip) {
			return false
		}
	}
	return true
}
