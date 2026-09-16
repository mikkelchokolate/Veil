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
	for _, ip := range ips {
		if addr, ok := netip.AddrFromSlice(ip); ok && diagnosticIPAllowed(addr) {
			return nil
		}
	}
	return reject
}

func diagnosticIPAllowed(addr netip.Addr) bool {
	addr = addr.Unmap()
	return !(addr.IsUnspecified() || addr.IsLoopback() ||
		addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() ||
		addr.IsMulticast())
}
