package generatedconfig

import "testing"

// TestMieruGeneratedConfigModelAllDisabledProfilesDoNotReviveInboundCredential
// covers audit #3: when profiles exist but every one is disabled, the inbound
// fallback credential must NOT be silently re-enabled.
func TestMieruGeneratedConfigModelAllDisabledProfilesDoNotReviveInboundCredential(t *testing.T) {
	config, ok, err := NewMieruGeneratedConfigModel(Settings{}).Build([]Inbound{
		{
			Name:      "mieru-a",
			Protocol:  "mieru",
			Transport: "tcp",
			Port:      443,
			Enabled:   true,
			Password:  "legacy-inbound-pass",
			Profiles:  []ClientProfile{{Name: "alice", Username: "alice", Password: "alice-pass", Enabled: false}},
		},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !ok {
		t.Fatal("expected Mieru config model")
	}
	if len(config.Users) != 0 {
		t.Fatalf("users = %+v, want none (all profiles disabled must revoke the user, not fall back)", config.Users)
	}
	// The binding is still rendered (the inbound is enabled).
	if len(config.PortBindings) != 1 {
		t.Fatalf("bindings = %+v", config.PortBindings)
	}
}

// TestMieruGeneratedConfigModelNoProfilesFallsBackToInboundCredential keeps the
// legacy contract: an inbound without any profiles uses its own credential.
func TestMieruGeneratedConfigModelNoProfilesFallsBackToInboundCredential(t *testing.T) {
	config, ok, err := NewMieruGeneratedConfigModel(Settings{}).Build([]Inbound{
		{Name: "mieru-a", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Password: "legacy-pass"},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !ok {
		t.Fatal("expected Mieru config model")
	}
	if len(config.Users) != 1 || config.Users[0].Name != "mieru-a" || config.Users[0].Password != "legacy-pass" {
		t.Fatalf("users = %+v", config.Users)
	}
}

func TestMieruGeneratedConfigModelAggregatesEnabledMieruBindingsAndUsers(t *testing.T) {
	config, ok, err := NewMieruGeneratedConfigModel(Settings{}).Build([]Inbound{
		{Name: "disabled", Protocol: "mieru", Transport: "tcp", Port: 80, Enabled: false, Password: "disabled"},
		{Name: "mieru-tcp", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Password: "tcp-pass"},
		{Name: "mieru-udp", Protocol: "mieru", Transport: "udp", Port: 443, Enabled: true, Profiles: []ClientProfile{{Name: "alice", Password: "alice-pass", Enabled: true}}},
		{Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 8443, Enabled: true, Password: "hy2-pass"},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !ok {
		t.Fatal("expected Mieru config model")
	}
	if len(config.PortBindings) != 2 || config.PortBindings[0].Protocol != "tcp" || config.PortBindings[1].Protocol != "udp" {
		t.Fatalf("bindings = %+v", config.PortBindings)
	}
	if len(config.Users) != 2 || config.Users[0].Name != "mieru-tcp" || config.Users[0].Password != "tcp-pass" || config.Users[1].Name != "alice" || config.Users[1].Password != "alice-pass" {
		t.Fatalf("users = %+v", config.Users)
	}
}

func TestMieruGeneratedConfigModelReturnsNotRenderableWhenNoEnabledMieru(t *testing.T) {
	_, ok, err := NewMieruGeneratedConfigModel(Settings{}).Build([]Inbound{{Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true}})
	if err != nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

func TestMieruGeneratedConfigModelRejectsDuplicateUsernamesAcrossInbounds(t *testing.T) {
	_, _, err := NewMieruGeneratedConfigModel(Settings{}).Build([]Inbound{
		{Name: "shared", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Password: "p1"},
		{Name: "shared", Protocol: "mieru", Transport: "udp", Port: 444, Enabled: true, Password: "p2"},
	})
	if err == nil {
		t.Fatal("expected duplicate username error across aggregated mieru inbounds")
	}
}

func TestMieruGeneratedConfigModelRejectsDuplicateProfileUsernames(t *testing.T) {
	_, _, err := NewMieruGeneratedConfigModel(Settings{}).Build([]Inbound{
		{Name: "a", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Profiles: []ClientProfile{{Name: "alice", Password: "x", Enabled: true}}},
		{Name: "b", Protocol: "mieru", Transport: "udp", Port: 444, Enabled: true, Profiles: []ClientProfile{{Name: "alice", Password: "y", Enabled: true}}},
	})
	if err == nil {
		t.Fatal("expected duplicate profile username error across aggregated mieru inbounds")
	}
}

func TestMieruGeneratedConfigModelAllowsDistinctUsernames(t *testing.T) {
	config, ok, err := NewMieruGeneratedConfigModel(Settings{}).Build([]Inbound{
		{Name: "a", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Password: "x"},
		{Name: "b", Protocol: "mieru", Transport: "udp", Port: 444, Enabled: true, Password: "y"},
	})
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if len(config.Users) != 2 {
		t.Fatalf("users = %+v", config.Users)
	}
}
