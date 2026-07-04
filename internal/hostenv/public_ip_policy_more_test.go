package hostenv

import (
	"net"
	"testing"
)

func TestIsPublicRejectsNilIP(t *testing.T) {
	policy := NewPublicIPPolicy()
	if policy.IsPublic(nil) {
		t.Fatalf("expected nil IP to be non-public")
	}
}

func TestIsPublicHandlesNilCgnatCIDR(t *testing.T) {
	policy := NewPublicIPPolicy()

	orig := cgnatCIDR
	cgnatCIDR = nil
	defer func() { cgnatCIDR = orig }()

	// A normal public address should still be considered public.
	if !policy.IsPublic(net.ParseIP("8.8.8.8")) {
		t.Fatalf("expected 8.8.8.8 to be public when CGNAT CIDR is nil")
	}
}

func TestIsPublicHandlesEmptyDocCIDRs(t *testing.T) {
	policy := NewPublicIPPolicy()

	orig := docCIDRs
	docCIDRs = nil
	defer func() { docCIDRs = orig }()

	// A normal public address should still be considered public.
	if !policy.IsPublic(net.ParseIP("8.8.8.8")) {
		t.Fatalf("expected 8.8.8.8 to be public when doc CIDRs are nil")
	}
}
