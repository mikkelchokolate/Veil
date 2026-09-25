package clientaccess

import "testing"

func TestNewClientProfilePasswordPolicyWithNilGeneratorUsesDefault(t *testing.T) {
	policy := NewClientProfilePasswordPolicy(nil)
	profiles, err := policy.Complete([]ClientProfile{{Name: "alice"}}, nil)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(profiles) != 1 || profiles[0].Password == "" {
		t.Fatalf("expected generated password, got %+v", profiles)
	}
}
