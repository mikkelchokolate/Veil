package acmeip

import (
	"context"
	"fmt"
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

// readTLSCertInfo is a test seam over runtime.ReadTLSCert.
var readTLSCertInfo = runtime.ReadTLSCert

// NeedsRenewal reports whether the certificate at certPath must be (re)issued
// as of now (time.Now() when zero). It is the single renewal gate shared by
// the repair flow, the management-apply hook and the in-daemon renewal
// worker so every caller applies identical semantics (#1169/#1170):
//
//   - missing, unreadable, unparsable or currently invalid → renew
//   - issued by something other than Let's Encrypt (e.g. the installer's
//     self-signed fallback) → renew
//   - NotAfter that cannot be parsed, or expiry inside RenewalWindow → renew
func NeedsRenewal(certPath string, now time.Time) bool {
	if now.IsZero() {
		now = time.Now()
	}
	info := readTLSCertInfo(certPath)
	if !info.Valid || info.Error != "" {
		return true
	}
	if !strings.Contains(info.Issuer, "Let's Encrypt") {
		return true
	}
	notAfter, err := time.Parse(time.RFC3339, info.NotAfter)
	if err != nil {
		return true
	}
	return !notAfter.After(now.Add(RenewalWindow))
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
