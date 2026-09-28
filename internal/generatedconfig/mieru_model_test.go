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
	// All-disabled profiles are full revocation: the inbound must NOT render
	// the legacy fallback credential, and — so the daemon does not even
	// listen on a port nobody can authenticate to — the port binding is
	// dropped too. With nothing left the model itself is omitted (ok=false)
	// so promotion stops the unit (issue #1098).
	if ok {
		t.Fatalf("expected the model to be dropped for a fully revoked inbound, got %+v", config)
	}
	if len(config.Users) != 0 || len(config.PortBindings) != 0 {
		t.Fatalf("revoked inbound leaked users/bindings: %+v", config)
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

// TestMieruGeneratedConfigModelClientBindingsDoNotReviveInboundCredential is
// the #1098 normalization analogue of the all-disabled-profiles case: the
// binding rows still exist but every usable credential is gone — the inbound
// drops out of the aggregate instead of resurrecting the fallback password.
func TestMieruGeneratedConfigModelClientBindingsDoNotReviveInboundCredential(t *testing.T) {
	config, ok, err := NewMieruGeneratedConfigModel(Settings{}).Build([]Inbound{
		{
			Name:      "mieru-a",
			Protocol:  "mieru",
			Transport: "tcp",
			Port:      443,
			Enabled:   true,
			Password:  "legacy-inbound-pass",
			// Equivalent to a binding list whose credentials are all revoked.
			HasClientBindings: true,
		},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if ok {
		t.Fatalf("expected the model to be dropped for a revoked client-managed inbound, got %+v", config)
	}
	if len(config.Users) != 0 || len(config.PortBindings) != 0 {
		t.Fatalf("revoked inbound leaked users/bindings: %+v", config)
	}
}
