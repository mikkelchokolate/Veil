package inbounds

import "testing"

func TestInboundPasswordPolicyUsesDefaultGeneratorWhenNil(t *testing.T) {
	policy := NewInboundPasswordPolicy(nil)
	inbound := Inbound{Name: "n", Protocol: "naiveproxy", Transport: "tcp", Port: 443}
	policy.ApplyCreate(&inbound)
	if inbound.Password == "" {
		t.Fatal("expected a generated password when generator is nil")
	}
}
