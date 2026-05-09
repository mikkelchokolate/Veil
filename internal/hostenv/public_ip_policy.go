package hostenv

import "net"

type PublicIPPolicy struct{}

func NewPublicIPPolicy() PublicIPPolicy { return PublicIPPolicy{} }

func (PublicIPPolicy) IsPublic(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() {
		return false
	}
	if cgnatCIDR != nil && cgnatCIDR.Contains(ip) {
		return false
	}
	for _, cidr := range docCIDRs {
		if cidr != nil && cidr.Contains(ip) {
			return false
		}
	}
	return true
}

func isPublicIP(ip net.IP) bool {
	return NewPublicIPPolicy().IsPublic(ip)
}
