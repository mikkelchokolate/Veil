package clientaccess

import (
	"errors"
	"testing"
)

func TestInboundCredentialPolicyApplyCreateNilInbound(t *testing.T) {
	policy := NewInboundCredentialPolicy(func() (string, error) { return "generated", nil })
	if err := policy.ApplyCreate(nil); err != nil {
		t.Fatalf("ApplyCreate(nil): %v", err)
	}
}

func TestInboundCredentialPolicyApplyCreateGeneratesInboundPassword(t *testing.T) {
	policy := NewInboundCredentialPolicy(func() (string, error) { return "generated-pass", nil })
	inbound := Inbound{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443}
	if err := policy.ApplyCreate(&inbound); err != nil {
		t.Fatalf("ApplyCreate: %v", err)
	}
	if inbound.Password != "generated-pass" {
		t.Fatalf("password = %q", inbound.Password)
	}
}

func TestInboundCredentialPolicyApplyUpdateNilInbound(t *testing.T) {
	policy := NewInboundCredentialPolicy(func() (string, error) { return "generated", nil })
	if err := policy.ApplyUpdate(nil, Inbound{}); err != nil {
		t.Fatalf("ApplyUpdate(nil): %v", err)
	}
}

// TestInboundCredentialPolicyPropagatesGeneratorError is the #1022
// regression: a generator failure must surface, not be dropped into a
// silently-empty credential.
func TestInboundCredentialPolicyPropagatesGeneratorError(t *testing.T) {
	sentinel := errors.New("rand failure")
	policy := NewInboundCredentialPolicy(func() (string, error) { return "", sentinel })

	inbound := Inbound{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443}
	if err := policy.ApplyCreate(&inbound); !errors.Is(err, sentinel) {
		t.Fatalf("ApplyCreate err = %v, want %v", err, sentinel)
	}
	if inbound.Password != "" {
		t.Fatalf("failed generation must leave password empty, got %q", inbound.Password)
	}

	withProfile := Inbound{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Password: "set", Profiles: []ClientProfile{{Name: "alice", Enabled: true}}}
	if err := policy.ApplyCreate(&withProfile); !errors.Is(err, sentinel) {
		t.Fatalf("ApplyCreate(profile) err = %v, want %v", err, sentinel)
	}
	if err := policy.ApplyUpdate(&withProfile, Inbound{}); !errors.Is(err, sentinel) {
		t.Fatalf("ApplyUpdate err = %v, want %v", err, sentinel)
	}
}
