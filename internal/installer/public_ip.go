package installer

import (
	"context"
	"fmt"
	"io"
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
		panic(err)
	}
	return cidr
}()

func isPublicIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	// Reject unspecified, loopback, private, link-local, multicast, and CGNAT.
	if ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || cgnatCIDR.Contains(ip) {
		return false
	}
	return true
}
