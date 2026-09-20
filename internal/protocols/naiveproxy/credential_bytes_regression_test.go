package naiveproxy

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

// TestNaivePasswordPreservesInboundPasswordBytes is the #331 regression: the
// server-side resolver used to TrimSpace(inbound.Password) while client
// export embedded the raw stored value, so an explicitly entered password
// with surrounding whitespace rendered a different credential than the URI
// advertised. The winning value must be preserved byte-for-byte.
func TestNaivePasswordPreservesInboundPasswordBytes(t *testing.T) {
	inbound := model.Inbound{Password: " padded-pass "}
	if got := naivePassword(model.Settings{}, inbound); got != " padded-pass " {
		t.Fatalf("naivePassword = %q, want the stored bytes %q", got, " padded-pass ")
	}
}

// TestNaivePasswordWhitespaceOnlyFallsThrough keeps the empty-check contract:
// a whitespace-only inbound password counts as unset and must not shadow the
// dynamic or settings-level credential.
func TestNaivePasswordWhitespaceOnlyFallsThrough(t *testing.T) {
	inbound := model.Inbound{Password: "  ", ProtocolFields: map[string]any{"naivePassword": "dynamic-secret"}}
	if got := naivePassword(model.Settings{}, inbound); got != "dynamic-secret" {
		t.Fatalf("naivePassword = %q, want %q", got, "dynamic-secret")
	}
}

// TestLiveNaiveUsersEmitsRuntimeCredentialBytes is the #334 regression: the
// Caddy forward_auth list trimmed runtime credentials while client export
// advertised the raw stored bytes. Stored bytes must be emitted as-is, and a
// normalized credential must override a legacy profile on the trimmed
// username.
func TestLiveNaiveUsersEmitsRuntimeCredentialBytes(t *testing.T) {
	inbound := model.Inbound{
		Name:     "naive-a",
		Protocol: "naiveproxy",
		Profiles: []model.ClientProfile{
			{Name: "alice", Username: " alice ", Password: "legacy-pass", Enabled: true},
		},
		RuntimeCredentials: []model.RuntimeCredential{
			{Name: "alice", Username: "alice", Password: " padded-cred "},
		},
	}
	users := liveNaiveUsers(model.Settings{}, inbound)
	if len(users) != 1 {
		t.Fatalf("liveNaiveUsers = %+v, want exactly one user (normalized credential overrides the trimmed-username profile)", users)
	}
	if users[0].Username != "alice" || users[0].Password != " padded-cred " {
		t.Fatalf("liveNaiveUsers = %+v, want stored credential bytes", users)
	}
}

// TestLiveNaiveUsersSkipsWhitespaceOnlyCredentials pins the eligibility rule
// shared with client export: a credential whose username or password trims to
// empty can never authenticate and must not be rendered or advertised.
func TestLiveNaiveUsersSkipsWhitespaceOnlyCredentials(t *testing.T) {
	inbound := model.Inbound{
		Name:     "naive-a",
		Protocol: "naiveproxy",
		RuntimeCredentials: []model.RuntimeCredential{
			{Name: "blank-pass", Username: "bob", Password: "   "},
		},
	}
	users := liveNaiveUsers(model.Settings{}, inbound)
	if len(users) != 0 {
		t.Fatalf("liveNaiveUsers = %+v, want no users for a whitespace-only credential", users)
	}
}
