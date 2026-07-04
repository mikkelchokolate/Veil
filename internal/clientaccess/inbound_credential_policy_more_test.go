package clientaccess

import "testing"

func TestInboundCredentialPolicyApplyCreateNilInbound(t *testing.T) {
	policy := NewInboundCredentialPolicy(func() string { return "generated" })
	policy.ApplyCreate(nil) // must not panic
}

func TestInboundCredentialPolicyApplyCreateGeneratesInboundPassword(t *testing.T) {
	policy := NewInboundCredentialPolicy(func() string { return "generated-pass" })
	inbound := Inbound{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443}
	policy.ApplyCreate(&inbound)
	if inbound.Password != "generated-pass" {
		t.Fatalf("password = %q", inbound.Password)
	}
}

func TestInboundCredentialPolicyApplyUpdateNilInbound(t *testing.T) {
	policy := NewInboundCredentialPolicy(func() string { return "generated" })
	policy.ApplyUpdate(nil, Inbound{}) // must not panic
}
