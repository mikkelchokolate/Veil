package generatedconfig

import "testing"

func TestMieruGeneratedConfigUsesEffectiveProtocolFieldPassword(t *testing.T) {
	const password = "dynamic-secret"
	model, ok, err := NewMieruGeneratedConfigModel(Settings{}).Build([]Inbound{{
		Name: "mieru-one", Protocol: "mieru", Transport: "tcp", Port: 23456, Enabled: true,
		ProtocolFields: map[string]any{"password": password},
	}})
	if err != nil || !ok {
		t.Fatalf("Build: ok=%v err=%v", ok, err)
	}
	if len(model.Users) != 1 || model.Users[0].Password != password {
		t.Fatalf("render users = %+v, want effective protocolFields password", model.Users)
	}
}
