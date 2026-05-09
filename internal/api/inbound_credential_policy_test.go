package api

import "testing"

func TestInboundCredentialPolicyCompletesCreateAndUpdateCredentials(t *testing.T) {
	generated := []string{"first", "second"}
	policy := NewInboundCredentialPolicy(func() string {
		value := generated[0]
		generated = generated[1:]
		return value
	})

	inbound := Inbound{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Profiles: []ClientProfile{{Name: "alice", Enabled: true}}}
	policy.ApplyCreate(&inbound)
	if inbound.Password != "" {
		t.Fatalf("inbound password should stay empty when client profiles exist: %+v", inbound)
	}
	if len(inbound.Profiles) != 1 || inbound.Profiles[0].Password != "first" {
		t.Fatalf("client profile password not generated: %+v", inbound.Profiles)
	}

	update := Inbound{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Profiles: []ClientProfile{{Name: "alice", Enabled: true}, {Name: "bob", Enabled: true}}}
	policy.ApplyUpdate(&update, inbound)
	if update.Profiles[0].Password != "first" || update.Profiles[1].Password != "second" {
		t.Fatalf("update profile passwords = %+v", update.Profiles)
	}
}

func TestInboundCredentialPolicyBuildsEnabledClientCredentials(t *testing.T) {
	credentials, err := NewInboundCredentialPolicy(nil).ClientCredentials(Inbound{Profiles: []ClientProfile{{Name: "alice", Enabled: true, Password: "secret"}, {Name: "disabled", Enabled: false, Password: "nope"}}})
	if err != nil {
		t.Fatalf("ClientCredentials: %v", err)
	}
	if len(credentials) != 1 || credentials[0].Username != "alice" || credentials[0].Password != "secret" {
		t.Fatalf("credentials = %+v", credentials)
	}
}
