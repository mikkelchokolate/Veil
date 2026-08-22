package clientaccess

import (
	"testing"

	model "github.com/mikkelchokolate/Veil/internal/model"
)

// TestClientAccessMergesNormalizedCredentials asserts (A6) that credentials
// resolved from the normalized client store are merged with (and take the same
// render path as) legacy inbound-embedded profiles, so a client that exists
// only as Client+Binding+Credential is still rendered into the live config.
func TestClientAccessMergesNormalizedCredentials(t *testing.T) {
	inbound := model.Inbound{Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true}
	normalized := []ClientCredential{{Name: "alice", Username: "alice", Password: "alice-pass"}}

	access, err := BuildClientAccess(model.Settings{}, inbound, WithCredentials(normalized))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	users := access.Hysteria2Users()
	if len(users) != 1 {
		t.Fatalf("expected 1 rendered user from normalized credentials, got %d", len(users))
	}
	if users[0].Username != "alice" || users[0].Password != "alice-pass" {
		t.Fatalf("wrong rendered user: %+v", users[0])
	}
}

// TestClientAccessNormalizedOverridesLegacyByUsername ensures a normalized
// credential replaces a legacy profile with the same username (single source
// of truth: the normalized store wins).
func TestClientAccessNormalizedOverridesLegacyByUsername(t *testing.T) {
	inbound := model.Inbound{Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true,
		Profiles: []model.ClientProfile{{Name: "alice", Username: "alice", Password: "legacy-pass", Enabled: true}}}
	normalized := []ClientCredential{{Name: "alice", Username: "alice", Password: "new-pass"}}

	access, err := BuildClientAccess(model.Settings{}, inbound, WithCredentials(normalized))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	users := access.Hysteria2Users()
	if len(users) != 1 {
		t.Fatalf("expected dedup to 1 user, got %d: %+v", len(users), users)
	}
	if users[0].Password != "new-pass" {
		t.Fatalf("normalized credential should win, got password %q", users[0].Password)
	}
}
