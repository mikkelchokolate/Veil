package api

import (
	"context"
	"errors"
	"net"
	"net/netip"
)

// diagnosticTargetLookup resolves a diagnostic hostname to literal addresses so
// the outbound-target policy can inspect every resolved IP before a tool runs.
var diagnosticTargetLookup = func(ctx context.Context, host string) ([]net.IP, error) {
	return net.DefaultResolver.LookupIP(ctx, "ip", host)
}

// validateDiagnosticTargetScope enforces the same outbound-destination policy
// the rest of Veil applies: diagnostic tools must not probe loopback,
// link-local (including cloud metadata endpoints), unspecified, or multicast
// addresses. Public and RFC1918 LAN targets stay allowed.
func validateDiagnosticTargetScope(ctx context.Context, target string) error {
	reject := errors.New("target must not be a loopback, link-local, unspecified, or multicast address")
	if addr, err := netip.ParseAddr(target); err == nil {
		if !diagnosticIPAllowed(addr) {
			return reject
		}
		return nil
	}
	ips, err := diagnosticTargetLookup(ctx, target)
	if err != nil {
		// Resolution failures surface through the tool's own error path.
		return nil
	}
	// A hostname is only allowed when EVERY resolved address is allowed: the
	// OS may pick any of them when the tool runs, so a single forbidden
	// address (link-local/metadata) in a mixed answer is enough to reject.
	usable := 0
	for _, ip := range ips {
		addr, ok := netip.AddrFromSlice(ip)
		if !ok {
			continue
		}
		usable++
		if !diagnosticIPAllowed(addr) {
			return reject
		}
	}
	if usable == 0 {
		return reject
	}
	return nil
}

func diagnosticIPAllowed(addr netip.Addr) bool {
	addr = addr.Unmap()
	return !(addr.IsUnspecified() || addr.IsLoopback() ||
		addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() ||
		addr.IsMulticast())
}
