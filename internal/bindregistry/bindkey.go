package bindregistry

import (
	"fmt"
	"net/netip"
	"strings"
)

// ListenNetwork is the network protocol used by a public listener.
type ListenNetwork string

const (
	ListenTCP ListenNetwork = "tcp"
	ListenUDP ListenNetwork = "udp"
)

// BindKey uniquely identifies a network listener after canonicalization.
type BindKey struct {
	Address string
	Port    int
	Network ListenNetwork
}

// BindOwnerKind identifies the subsystem that owns a listener.
type BindOwnerKind string

const (
	BindOwnerPanelDirect   BindOwnerKind = "panel_direct"
	BindOwnerPanelCaddy    BindOwnerKind = "panel_caddy"
	BindOwnerNaive         BindOwnerKind = "naive"
	BindOwnerHysteria2     BindOwnerKind = "hysteria2"
	BindOwnerInbound       BindOwnerKind = "inbound"
	BindOwnerLegacyCaddy   BindOwnerKind = "legacy_caddy"
	BindOwnerAcmeChallenge BindOwnerKind = "acme_challenge"
)

// BindOwner describes the service or inbound responsible for a listener.
type BindOwner struct {
	Kind        BindOwnerKind
	ServiceName string
	InboundName string
}

// NormalizeAddress returns a stable textual representation for a bind address.
// Empty addresses and * are treated as the public IPv4 wildcard.
func NormalizeAddress(addr string) string {
	addr = strings.TrimSpace(addr)
	switch addr {
	case "", "*", "0.0.0.0":
		return "0.0.0.0"
	case "::", "[::]":
		return "::"
	}

	if strings.HasPrefix(addr, "[") && strings.HasSuffix(addr, "]") {
		addr = strings.TrimSuffix(strings.TrimPrefix(addr, "["), "]")
	}
	if parsed, err := netip.ParseAddr(addr); err == nil {
		return parsed.Unmap().String()
	}
	return strings.ToLower(addr)
}

// IsWildcard reports whether addr represents an IPv4 or IPv6 wildcard.
func IsWildcard(addr string) bool {
	switch NormalizeAddress(addr) {
	case "0.0.0.0", "::":
		return true
	default:
		return false
	}
}

// Canonical returns the normalized form of k. Use Validate before persisting a
// user-supplied key.
func (k BindKey) Canonical() BindKey {
	return BindKey{
		Address: NormalizeAddress(k.Address),
		Port:    k.Port,
		Network: ListenNetwork(strings.ToLower(strings.TrimSpace(string(k.Network)))),
	}
}

// Validate checks that the key can represent a real TCP or UDP listener.
func (k BindKey) Validate() error {
	canonical := k.Canonical()
	if canonical.Port < 1 || canonical.Port > 65535 {
		return fmt.Errorf("invalid bind port %d: must be between 1 and 65535", canonical.Port)
	}
	if canonical.Network != ListenTCP && canonical.Network != ListenUDP {
		return fmt.Errorf("invalid listen network %q: must be tcp or udp", canonical.Network)
	}
	if _, err := netip.ParseAddr(canonical.Address); err != nil {
		return fmt.Errorf("invalid bind address %q: %w", canonical.Address, err)
	}
	return nil
}

// Overlaps reports whether two keys may claim the same operating-system
// listener. IPv6 wildcard is treated conservatively as dual-stack because the
// runtime may use IPV6_V6ONLY=0.
func (k BindKey) Overlaps(other BindKey) bool {
	a := k.Canonical()
	b := other.Canonical()
	if a.Port != b.Port || a.Network != b.Network {
		return false
	}
	if a.Address == b.Address {
		return true
	}

	aAddr, aErr := netip.ParseAddr(a.Address)
	bAddr, bErr := netip.ParseAddr(b.Address)
	if aErr != nil || bErr != nil {
		// Invalid keys are rejected by Validate. Keep overlap checking
		// conservative if an unchecked key reaches this method.
		return IsWildcard(a.Address) || IsWildcard(b.Address)
	}
	aAddr = aAddr.Unmap()
	bAddr = bAddr.Unmap()

	if IsWildcard(a.Address) || IsWildcard(b.Address) {
		if aAddr.Is6() && IsWildcard(a.Address) {
			return true
		}
		if bAddr.Is6() && IsWildcard(b.Address) {
			return true
		}
		return aAddr.Is4() == bAddr.Is4()
	}
	return false
}

func (k BindKey) String() string {
	canonical := k.Canonical()
	return fmt.Sprintf("%s %s:%d", canonical.Network, canonical.Address, canonical.Port)
}
