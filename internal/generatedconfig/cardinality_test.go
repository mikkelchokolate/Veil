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

func TestGeneratedConfigCardinalityRejectsMieruWithNoUsableUsers(t *testing.T) {
	registry := NewProtocolRegistry([]Protocol{{Protocol: "mieru"}})
	err := NewGeneratedConfigCardinality(Settings{}, registry).Validate([]Inbound{{
		Name:     "mieru",
		Protocol: "mieru",
		Enabled:  true,
		Password: "leftover",
		Profiles: []ClientProfile{{Name: "alice", Username: "alice", Password: "alice-pass", Enabled: false}},
	}})
	if err == nil || err.Error() != "this inbound has no usable client credential" {
		t.Fatalf("err = %v", err)
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
		{Name: "b", Protocol: "mieru", Enabled: true},
		{Name: "c", Protocol: "naiveproxy", Enabled: false},
		{Name: "d", Protocol: "mieru", Enabled: true},
	})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
}
