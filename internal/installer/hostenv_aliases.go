package installer

import (
	"context"
	"net"
	"net/http"

	"github.com/veil-panel/veil/internal/hostenv"
)

type Platform = hostenv.Platform
type DNSResolver = hostenv.DNSResolver
type NetResolver = hostenv.NetResolver
type DNSCheck = hostenv.DNSCheck
type PublicIPPolicy = hostenv.PublicIPPolicy

func CurrentPlatform() Platform                     { return hostenv.CurrentPlatform() }
func NormalizeArch(arch string) (string, error)     { return hostenv.NormalizeArch(arch) }
func ValidateLinuxPlatform(platform Platform) error { return hostenv.ValidateLinuxPlatform(platform) }
func ValidateDomain(domain string) error            { return hostenv.ValidateDomain(domain) }
func ValidateEmail(email string) error              { return hostenv.ValidateEmail(email) }
func CheckDomainDNS(ctx context.Context, resolver DNSResolver, domain string, publicIP net.IP) (DNSCheck, error) {
	return hostenv.CheckDomainDNS(ctx, resolver, domain, publicIP)
}
func DefaultPublicIPEndpoints() []string { return hostenv.DefaultPublicIPEndpoints() }
func DetectPublicIP(ctx context.Context, client *http.Client, endpoints []string) (net.IP, error) {
	return hostenv.DetectPublicIP(ctx, client, endpoints)
}
func NewPublicIPPolicy() PublicIPPolicy { return hostenv.NewPublicIPPolicy() }
