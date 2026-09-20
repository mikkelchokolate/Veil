package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

// diagnosticTargetLookup resolves a diagnostic hostname to literal addresses so
// the outbound-target policy can inspect every resolved IP before a tool runs.
var diagnosticTargetLookup = func(ctx context.Context, host string) ([]net.IP, error) {
	return net.DefaultResolver.LookupIP(ctx, "ip", host)
}

// diagnosticTargetResolution is the scope-approved resolution of one tool
// target. The probe is always a literal IP so a tool can never be rebound to a
// different destination after the policy check (#575).
type diagnosticTargetResolution struct {
	// probe is the pinned literal the tool must target: the canonical form for
	// IP inputs, or the first approved resolved address for hostnames.
	probe string
	// addrs are every approved address the target resolved to.
	addrs []netip.Addr
	// literal reports whether the target itself was an IP literal (canonical or
	// inet_aton shorthand) rather than a hostname.
	literal bool
}

// resolveDiagnosticTarget enforces the same outbound-destination policy the
// rest of Veil applies: diagnostic tools must not probe loopback, link-local
// (including cloud metadata endpoints), unspecified, or multicast addresses.
// Public and RFC1918 LAN targets stay allowed.
//
// The check fails closed: a hostname is resolved once here, EVERY resolved
// address must be allowed, and the returned probe is a literal so DNS cannot
// rebind to a forbidden address between the check and the probe.
func resolveDiagnosticTarget(ctx context.Context, target string) (diagnosticTargetResolution, error) {
	reject := errors.New("target must not be a loopback, link-local, unspecified, or multicast address")
	if addr, err := netip.ParseAddr(target); err == nil {
		addr = addr.Unmap()
		if !diagnosticIPAllowed(addr) {
			return diagnosticTargetResolution{}, reject
		}
		return diagnosticTargetResolution{probe: addr.String(), addrs: []netip.Addr{addr}, literal: true}, nil
	}
	// inet_aton shorthand (127.1, 2130706433, 0xa9fea9fe, 0177.0.0.1) is not a
	// canonical literal but IS interpreted as an address by ping and friends.
	// Canonicalize before the scope check so those forms cannot smuggle a
	// forbidden destination past the parser (#574).
	if addr, ok := parseDiagnosticIPv4Shorthand(target); ok {
		if !diagnosticIPAllowed(addr) {
			return diagnosticTargetResolution{}, reject
		}
		return diagnosticTargetResolution{probe: addr.String(), addrs: []netip.Addr{addr}, literal: true}, nil
	}
	ips, err := diagnosticTargetLookup(ctx, target)
	if err != nil {
		// Fail closed on resolution errors: allowing here would let a
		// transient (or attacker-influenced) lookup failure pass a hostname
		// that re-resolves to a forbidden target inside the tool (#374).
		return diagnosticTargetResolution{}, fmt.Errorf("target %q could not be resolved: %w", target, err)
	}
	var addrs []netip.Addr
	for _, ip := range ips {
		addr, ok := netip.AddrFromSlice(ip)
		if !ok {
			continue
		}
		addr = addr.Unmap()
		if !diagnosticIPAllowed(addr) {
			// Any forbidden address in the answer is grounds to reject the
			// whole target — the OS could still pick it at dial time (#357).
			return diagnosticTargetResolution{}, reject
		}
		addrs = append(addrs, addr)
	}
	if len(addrs) == 0 {
		return diagnosticTargetResolution{}, errors.New("target resolved to no usable addresses")
	}
	return diagnosticTargetResolution{probe: addrs[0].String(), addrs: addrs}, nil
}

func diagnosticIPAllowed(addr netip.Addr) bool {
	addr = addr.Unmap()
	return !(addr.IsUnspecified() || addr.IsLoopback() ||
		addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() ||
		addr.IsMulticast())
}

// parseDiagnosticIPv4Shorthand parses the non-canonical IPv4 forms accepted by
// inet_aton: one to four dot-separated components in decimal, octal (0-prefixed)
// or hex (0x-prefixed), where the final component fills the remaining bytes
// (e.g. "127.1" == 127.0.0.1, "2130706433" == 127.0.0.1, "0xa9fea9fe" ==
// 169.254.169.254).
func parseDiagnosticIPv4Shorthand(target string) (netip.Addr, bool) {
	parts := strings.Split(target, ".")
	if len(parts) > 4 {
		return netip.Addr{}, false
	}
	var value uint64
	for i, part := range parts {
		if part == "" {
			return netip.Addr{}, false
		}
		base := 10
		digits := part
		if len(digits) > 2 && (digits[:2] == "0x" || digits[:2] == "0X") {
			base, digits = 16, digits[2:]
		} else if len(digits) > 1 && digits[0] == '0' {
			base, digits = 8, digits[1:]
		}
		if digits == "" {
			return netip.Addr{}, false
		}
		component, err := strconv.ParseUint(digits, base, 64)
		if err != nil {
			return netip.Addr{}, false
		}
		if i < len(parts)-1 {
			if component > 0xff {
				return netip.Addr{}, false
			}
			value = (value << 8) | component
			continue
		}
		// The last component occupies 8*(5-len(parts)) bits.
		if component > (uint64(1)<<(8*(5-len(parts))))-1 {
			return netip.Addr{}, false
		}
		value = (value << (8 * (5 - len(parts)))) | component
	}
	if value > 0xffffffff {
		return netip.Addr{}, false
	}
	return netip.AddrFrom4([4]byte{byte(value >> 24), byte(value >> 16), byte(value >> 8), byte(value)}), true
}
