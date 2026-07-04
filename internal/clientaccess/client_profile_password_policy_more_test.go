package clientaccess

import "testing"

func TestNewClientProfilePasswordPolicyWithNilGeneratorUsesDefault(t *testing.T) {
	policy := NewClientProfilePasswordPolicy(nil)
	profiles := policy.Complete([]ClientProfile{{Name: "alice"}}, nil)
	if len(profiles) != 1 || profiles[0].Password == "" {
		t.Fatalf("expected generated password, got %+v", profiles)
	}
}
