package generatedconfig

import "testing"

func TestGeneratedConfigCardinalityRejectsMultipleEnabledSameProtocol(t *testing.T) {
	registry := NewProtocolRegistry([]Protocol{{Protocol: "naiveproxy", MaxEnabled: 1}})
	err := NewGeneratedConfigCardinality(Settings{}, registry).Validate([]Inbound{
		{Name: "a", Protocol: "naiveproxy", Enabled: true},
		{Name: "b", Protocol: "naiveproxy", Enabled: true},
	})
	if err == nil || err.Error() != "multiple enabled naiveproxy inbounds are not renderable as a single generated config yet" {
		t.Fatalf("err = %v", err)
	}
}

func TestGeneratedConfigCardinalityDropsFullyRevokedMieruInbound(t *testing.T) {
	// A credential-managed inbound with zero usable users is deliberate
	// revocation, not misconfiguration: the aggregate model drops it (and the
	// unit stops on apply) instead of reviving the fallback password or
	// erroring the whole render (issue #1098).
	registry := NewProtocolRegistry([]Protocol{{Protocol: "mieru"}})
	err := NewGeneratedConfigCardinality(Settings{}, registry).Validate([]Inbound{{
		Name:     "mieru",
		Protocol: "mieru",
		Enabled:  true,
		Password: "leftover",
		Profiles: []ClientProfile{{Name: "alice", Username: "alice", Password: "alice-pass", Enabled: false}},
	}})
	if err != nil {
		t.Fatalf("err = %v, want nil — a fully revoked inbound must drop, not error", err)
	}
}

func TestGeneratedConfigCardinalityAllowsMieruWhenSiblingHasUsers(t *testing.T) {
	registry := NewProtocolRegistry([]Protocol{{Protocol: "mieru"}})
	err := NewGeneratedConfigCardinality(Settings{}, registry).Validate([]Inbound{
		{
			Name: "tcp", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true,
			Profiles: []ClientProfile{{Name: "alice", Username: "alice", Password: "alice-pass", Enabled: true}},
		},
		{
			Name: "alice", Protocol: "mieru", Transport: "udp", Port: 443, Enabled: true, Password: "leftover",
			Profiles: []ClientProfile{{Name: "bob", Username: "bob", Password: "bob-pass", Enabled: false}},
		},
	})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestGeneratedConfigCardinalityIgnoresDisabledAndProtocolsWithoutLimit(t *testing.T) {
	registry := NewProtocolRegistry([]Protocol{{Protocol: "naiveproxy", MaxEnabled: 1}, {Protocol: "mieru"}})
	err := NewGeneratedConfigCardinality(Settings{}, registry).Validate([]Inbound{
		{Name: "a", Protocol: "naiveproxy", Enabled: true},
		{Name: "b", Protocol: "mieru", Enabled: true, Password: "pw"},
		{Name: "c", Protocol: "naiveproxy", Enabled: false},
		{Name: "d", Protocol: "mieru", Enabled: true, Password: "pw"},
	})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
}
