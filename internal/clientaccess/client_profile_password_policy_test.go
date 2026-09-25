package clientaccess

import "testing"

func TestClientProfilePasswordPolicyPreservesPreviousOrGeneratesMissingPasswords(t *testing.T) {
	policy := NewClientProfilePasswordPolicy(func() (string, error) { return "generated", nil })
	profiles, err := policy.Complete(
		[]ClientProfile{{Name: "alice"}, {Name: "bob", Password: "explicit"}, {Name: "carol"}},
		[]ClientProfile{{Name: "alice", Password: "old"}},
	)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if profiles[0].Password != "old" || profiles[1].Password != "explicit" || profiles[2].Password != "generated" {
		t.Fatalf("profiles = %+v", profiles)
	}
}

func TestClientProfilePasswordPolicyDoesNotMutateInput(t *testing.T) {
	input := []ClientProfile{{Name: "alice"}}
	if _, err := NewClientProfilePasswordPolicy(func() (string, error) { return "generated", nil }).Complete(input, nil); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if input[0].Password != "" {
		t.Fatalf("input mutated: %+v", input)
	}
}
