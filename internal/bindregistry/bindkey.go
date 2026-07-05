package bindregistry

import "strings"

type ListenNetwork string

const (
	ListenTCP ListenNetwork = "tcp"
	ListenUDP ListenNetwork = "udp"
)

type BindKey struct {
	Address string
	Port    int
	Network ListenNetwork
}

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

type BindOwner struct {
	Kind        BindOwnerKind
	ServiceName string
	InboundName string
}

func NormalizeAddress(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "0.0.0.0"
	}
	return addr
}

func IsWildcard(addr string) bool {
	return addr == "0.0.0.0" || addr == "::" || addr == ""
}

func (k BindKey) Canonical() BindKey {
	return BindKey{Address: NormalizeAddress(k.Address), Port: k.Port, Network: k.Network}
}

func (k BindKey) Overlaps(other BindKey) bool {
	a := k.Canonical()
	b := other.Canonical()
	if a.Port != b.Port || a.Network != b.Network {
		return false
	}
	if a.Address == b.Address {
		return true
	}
	if IsWildcard(a.Address) || IsWildcard(b.Address) {
		return true
	}
	return false
}
