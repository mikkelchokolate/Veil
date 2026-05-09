package generatedconfig

import "testing"

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
