package installer

import (
	"context"
	"fmt"
	"net"
)

type DNSResolver interface {
	LookupIP(ctx context.Context, host string) ([]net.IP, error)
}

type NetResolver struct{}

func (NetResolver) LookupIP(ctx context.Context, host string) ([]net.IP, error) {
	return net.DefaultResolver.LookupIP(ctx, "ip", host)
}

type DNSCheck struct {
	Domain          string
	ResolvedIPs     []string
	PublicIP        string
	MatchesPublicIP bool
	Warnings        []string
}

func CheckDomainDNS(ctx context.Context, resolver DNSResolver, domain string, publicIP net.IP) (DNSCheck, error) {
	if err := ValidateDomain(domain); err != nil {
		return DNSCheck{}, err
	}
	if resolver == nil {
		resolver = NetResolver{}
	}
	ips, err := resolver.LookupIP(ctx, domain)
	if err != nil {
		return DNSCheck{}, err
	}
	check := DNSCheck{Domain: domain}
	if publicIP != nil {
		check.PublicIP = publicIP.String()
	}
	for _, ip := range ips {
		if ip == nil {
			continue
		}
		check.ResolvedIPs = append(check.ResolvedIPs, ip.String())
		if publicIP != nil && ip.Equal(publicIP) {
			check.MatchesPublicIP = true
		}
	}
	if publicIP != nil && !check.MatchesPublicIP {
		check.Warnings = append(check.Warnings, fmt.Sprintf("domain %s does not resolve to public IP %s", domain, publicIP.String()))
	}
	if len(check.ResolvedIPs) == 0 {
		check.Warnings = append(check.Warnings, fmt.Sprintf("domain %s has no A/AAAA records", domain))
	}
	return check, nil
}
