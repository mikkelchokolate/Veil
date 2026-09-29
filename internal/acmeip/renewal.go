package acmeip

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/mikkelchokolate/Veil/internal/hostenv"
	"github.com/mikkelchokolate/Veil/internal/runtime"
)

// RenewalWindow is how close to expiry a Let's Encrypt IP certificate may
// drift before Veil (re)issues it. The "shortlived" profile produces ~6-day
// certificates, so a 3-day window leaves roughly half the lifetime as
// headroom against a broken renewal channel while a freshly issued
// certificate still reads as healthy.
const RenewalWindow = 72 * time.Hour

// readManagedTLSCertInfo is a test seam over
// runtime.ReadManagedTLSCertForDomain.
var readManagedTLSCertInfo = runtime.ReadManagedTLSCertForDomain

// NeedsRenewal reports whether the certificate at certPath must be (re)issued
// as of now (time.Now() when zero). It is the single renewal gate shared by
// the repair flow, the management-apply hook and the in-daemon renewal
// worker so every caller applies identical semantics (#1169/#1170):
//
//   - missing, unreadable, unparsable or currently invalid → renew
//   - not issued by an ACME CA → renew. The classification is issuer-agnostic
//     (#1185): the installer's self-signed fallback and Caddy's internal
//     local CA must both be replaced, but a leaf issued by a controlled or
//     private CA configured via VEIL_ACME_CA_URL is just as managed as a
//     Let's Encrypt issuance — gating on the Let's Encrypt brand made valid
//     private-CA certificates renew in a loop.
//   - NotAfter that cannot be parsed, or expiry inside RenewalWindow → renew
func NeedsRenewal(certPath string, now time.Time) bool {
	if now.IsZero() {
		now = time.Now()
	}
	info := readManagedTLSCertInfo(certPath, "")
	if !info.Valid || info.Error != "" {
		return true
	}
	if info.Source != runtime.TLSCertSourceACME {
		return true
	}
	notAfter, err := time.Parse(time.RFC3339, info.NotAfter)
	if err != nil {
		return true
	}
	return !notAfter.After(now.Add(RenewalWindow))
}

// ParsePublicIPSpec parses a persisted public-IP specification —
// "ipv4", "ipv6" or "ipv4,ipv6" — into the per-family request slots. It
// performs no network access, so callers can reuse an install-time choice
// without ever probing the external detection endpoints (#1186).
func ParsePublicIPSpec(spec string) (publicIPv4, publicIPv6 string, err error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", "", nil
	}
	for _, part := range strings.FieldsFunc(spec, func(r rune) bool { return r == ',' || r == ' ' }) {
		ip := net.ParseIP(strings.TrimSpace(part))
		if ip == nil {
			return "", "", fmt.Errorf("public IP %q is not a valid IP address", part)
		}
		if ip.To4() != nil {
			if publicIPv4 != "" {
				return "", "", fmt.Errorf("public IP spec %q lists multiple IPv4 addresses", spec)
			}
			publicIPv4 = ip.String()
			continue
		}
		if publicIPv6 != "" {
			return "", "", fmt.Errorf("public IP spec %q lists multiple IPv6 addresses", spec)
		}
		publicIPv6 = ip.String()
	}
	if publicIPv4 == "" && publicIPv6 == "" {
		return "", "", fmt.Errorf("public IP spec %q contains no IP address", spec)
	}
	return publicIPv4, publicIPv6, nil
}

// ManagedCertIPIdentities returns the IP SANs of the certificate at certPath
// when — and only when — that certificate classifies as ACME-managed
// (TLSCertSourceACME). The installer's self-signed fallback carries loopback
// and private interface addresses that must never steer issuance, and an
// unrelated operator-installed certificate is not Veil's identity either;
// only the SANs of an already-issued ACME certificate describe the identity
// a renewal must preserve (#1186).
func ManagedCertIPIdentities(certPath string) []string {
	data, err := os.ReadFile(certPath)
	if err != nil {
		return nil
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil
	}
	if runtime.ClassifyManagedCertSource(leaf) != runtime.TLSCertSourceACME {
		return nil
	}
	ips := make([]string, 0, len(leaf.IPAddresses))
	for _, ip := range leaf.IPAddresses {
		if ip != nil {
			ips = append(ips, ip.String())
		}
	}
	return ips
}

// publicIPResolver and publicIPFamilyProber are test seams over the host
// detection helpers so issuer callers can be exercised without network
// access — the same pattern install/repair use (#665).
var (
	publicIPResolver      = hostenv.ResolvePublicIP
	publicIPFamilyProber  = hostenv.DetectPublicIPForFamily
	defaultIPEndpoints    = hostenv.DefaultPublicIPEndpoints
	detectPublicIPTimeout = 5 * time.Second
)

// ResolvePublicIPs resolves the public addresses an IP certificate must
// cover. value mirrors the CLI convention: empty or "auto" detects the
// primary family and then probes the other family best-effort so dual-stack
// hosts get both SANs on one certificate; an explicit literal is returned in
// its own family slot (an IPv6 literal must never occupy PublicIPv4, #665).
func ResolvePublicIPs(ctx context.Context, value string) (publicIPv4, publicIPv6 string, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	value = strings.TrimSpace(value)
	autoDetected := value == "" || strings.EqualFold(value, "auto")
	if autoDetected {
		value = "auto"
	}
	resolved, err := publicIPResolver(ctx, value, nil, defaultIPEndpoints())
	if err != nil {
		return "", "", err
	}
	if resolved == nil {
		return "", "", fmt.Errorf("public IP detection returned empty")
	}
	if resolved.To4() != nil {
		publicIPv4 = resolved.String()
	} else {
		publicIPv6 = resolved.String()
	}
	// An auto-detected single answer covers only the family the winning
	// endpoint connection used; probe the other family so the certificate
	// covers both public identities when the host has both. The probe is
	// best-effort — a single-stack host simply leaves the field empty.
	if autoDetected {
		otherNetwork := "tcp6"
		if publicIPv4 == "" {
			otherNetwork = "tcp4"
		}
		probeCtx, cancel := context.WithTimeout(ctx, detectPublicIPTimeout)
		other, probeErr := publicIPFamilyProber(probeCtx, defaultIPEndpoints(), otherNetwork)
		cancel()
		if probeErr == nil && other != nil {
			if other.To4() != nil {
				publicIPv4 = other.String()
			} else {
				publicIPv6 = other.String()
			}
		}
	}
	return publicIPv4, publicIPv6, nil
}
