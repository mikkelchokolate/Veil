package clientaccess

import "testing"

func TestClientProfilePasswordPolicyPreservesPreviousOrGeneratesMissingPasswords(t *testing.T) {
	policy := NewClientProfilePasswordPolicy(func() string { return "generated" })
	profiles := policy.Complete(
		[]ClientProfile{{Name: "alice"}, {Name: "bob", Password: "explicit"}, {Name: "carol"}},
		[]ClientProfile{{Name: "alice", Password: "old"}},
	)
	if profiles[0].Password != "old" || profiles[1].Password != "explicit" || profiles[2].Password != "generated" {
		t.Fatalf("profiles = %+v", profiles)
	}
}

func TestClientProfilePasswordPolicyDoesNotMutateInput(t *testing.T) {
	input := []ClientProfile{{Name: "alice"}}
	_ = NewClientProfilePasswordPolicy(func() string { return "generated" }).Complete(input, nil)
	if input[0].Password != "" {
		t.Fatalf("input mutated: %+v", input)
	}
}
