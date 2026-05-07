package api

import "testing"

func TestInboundPasswordPolicyGeneratesCreatePasswordWhenNoProfiles(t *testing.T) {
	policy := NewInboundPasswordPolicy(func() string { return "generated" })
	inbound := Inbound{Name: "n", Protocol: "naiveproxy", Transport: "tcp", Port: 443}
	policy.ApplyCreate(&inbound)
	if inbound.Password != "generated" {
		t.Fatalf("password = %q", inbound.Password)
	}
}

func TestInboundPasswordPolicyPreservesUpdatePassword(t *testing.T) {
	policy := NewInboundPasswordPolicy(func() string { return "generated" })
	inbound := Inbound{Name: "n", Protocol: "naiveproxy", Transport: "tcp", Port: 443}
	policy.ApplyUpdate(&inbound, Inbound{Password: "old"})
	if inbound.Password != "old" {
		t.Fatalf("password = %q", inbound.Password)
	}
}

func TestInboundPasswordPolicyDoesNotGenerateCreatePasswordWhenProfilesExist(t *testing.T) {
	policy := NewInboundPasswordPolicy(func() string { return "generated" })
	inbound := Inbound{Name: "n", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Profiles: []ClientProfile{{Name: "alice"}}}
	policy.ApplyCreate(&inbound)
	if inbound.Password != "" {
		t.Fatalf("password = %q", inbound.Password)
	}
}
