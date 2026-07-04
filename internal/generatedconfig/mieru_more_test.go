package generatedconfig

import "testing"

func TestMieruGeneratedConfigModelReturnsClientCredentialsError(t *testing.T) {
	_, _, err := NewMieruGeneratedConfigModel(Settings{}).Build([]Inbound{
		{Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Profiles: []ClientProfile{{Name: "alice", Enabled: true}}},
	})
	if err == nil {
		t.Fatal("expected error for profile missing password")
	}
}

func TestGeneratedMieruConfigRendererReturnsRenderError(t *testing.T) {
	applyRoot := t.TempDir()
	_, _, err := NewGeneratedMieruConfigRenderer(Settings{}, NewPaths(applyRoot)).Render([]Inbound{
		{Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: 0, Enabled: true, Password: "pass"},
	})
	if err == nil {
		t.Fatal("expected render error for invalid port")
	}
}

func TestGeneratedMieruConfigRendererPropagatesBuildError(t *testing.T) {
	applyRoot := t.TempDir()
	_, _, err := NewGeneratedMieruConfigRenderer(Settings{}, NewPaths(applyRoot)).Render([]Inbound{
		{Name: "shared", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Password: "p1"},
		{Name: "shared", Protocol: "mieru", Transport: "udp", Port: 444, Enabled: true, Password: "p2"},
	})
	if err == nil {
		t.Fatal("expected build error for duplicate user name")
	}
}
