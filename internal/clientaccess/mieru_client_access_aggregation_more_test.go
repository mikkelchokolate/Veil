package clientaccess

import "testing"

func TestMieruAggregatorSkipsDisabledAndEmptyPassword(t *testing.T) {
	links, err := NewMieruClientAccessAggregator().Build(Settings{Domain: "vpn.example.com"}, []Inbound{
		{Name: "disabled", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: false},
		{Name: "empty", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(links) != 0 {
		t.Fatalf("expected no links, got %+v", links)
	}
}

func TestMieruAggregatorReturnsCredentialError(t *testing.T) {
	_, err := NewMieruClientAccessAggregator().Build(Settings{Domain: "vpn.example.com"}, []Inbound{
		{Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Profiles: []ClientProfile{{Name: "alice", Enabled: true}}},
	})
	if err == nil {
		t.Fatal("expected error for invalid profile credentials")
	}
}

func TestMieruAggregatorBuildNoDomain(t *testing.T) {
	links, err := NewMieruClientAccessAggregator().Build(Settings{}, []Inbound{
		{Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Password: "pass"},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(links) != 0 {
		t.Fatalf("expected no links without domain, got %+v", links)
	}
}

func TestMieruClientAccessProfileLinkNameFallsBackToUsername(t *testing.T) {
	name := mieruClientAccessProfileLinkName(ClientCredential{Name: "", Username: "alice", Password: "pass"})
	if name != "mieru/alice" {
		t.Fatalf("name = %q, want mieru/alice", name)
	}
}
