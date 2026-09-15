package generatedconfig

import (
	"strings"
	"testing"
)

func TestMieruGeneratedConfigModelReturnsClientCredentialsError(t *testing.T) {
	_, _, err := NewMieruGeneratedConfigModel(Settings{}).Build([]Inbound{
		{Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Profiles: []ClientProfile{{Name: "alice", Enabled: true}}},
	})
	if err == nil {
		t.Fatal("expected error for profile missing password")
	}
}

func TestGeneratedMieruConfigRendererSkipsAllDisabledProfiles(t *testing.T) {
	applyRoot := t.TempDir()
	artifact, ok, err := NewGeneratedMieruConfigRenderer(Settings{}, NewPaths(applyRoot)).Render([]Inbound{{
		Name:      "mieru",
		Protocol:  "mieru",
		Transport: "tcp",
		Port:      443,
		Enabled:   true,
		Password:  "leftover",
		Profiles:  []ClientProfile{{Name: "alice", Username: "alice", Password: "alice-pass", Enabled: false}},
	}})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if ok || artifact.Body != "" {
		t.Fatalf("all-disabled inbound must skip isolated render, got ok=%v body=%q", ok, artifact.Body)
	}
}

func TestGeneratedMieruConfigRendererKeepsSiblingUsersAndBindings(t *testing.T) {
	applyRoot := t.TempDir()
	artifact, ok, err := NewGeneratedMieruConfigRenderer(Settings{}, NewPaths(applyRoot)).Render([]Inbound{
		{
			Name: "tcp", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true,
			Profiles: []ClientProfile{{Name: "alice", Username: "alice", Password: "alice-pass", Enabled: true}},
		},
		{
			Name: "alice", Protocol: "mieru", Transport: "udp", Port: 443, Enabled: true, Password: "leftover",
			Profiles: []ClientProfile{{Name: "bob", Username: "bob", Password: "bob-pass", Enabled: false}},
		},
	})
	if err != nil || !ok {
		t.Fatalf("Render ok=%v err=%v", ok, err)
	}
	if !strings.Contains(artifact.Body, `"name": "alice"`) || !strings.Contains(artifact.Body, `"protocol": "TCP"`) || !strings.Contains(artifact.Body, `"protocol": "UDP"`) {
		t.Fatalf("aggregated config missing alice or both bindings:\n%s", artifact.Body)
	}
	if strings.Contains(artifact.Body, "leftover") {
		t.Fatalf("all-disabled inbound revived leftover password:\n%s", artifact.Body)
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
