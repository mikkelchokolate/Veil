package caddyassembly_test

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/bindregistry"
	"github.com/mikkelchokolate/Veil/internal/caddyassembly"
	"github.com/mikkelchokolate/Veil/internal/model"
)

// planNaiveUsers builds the final render plan and returns the forward_auth
// users carried by the naive TCP :443 server. Install, repair, panel access
// and the apply-plan validator render Caddy JSON straight from these users,
// so they are production output, not an intermediate value (#1404 review).
func planNaiveUsers(t *testing.T, settings model.Settings, inbound model.Inbound) []caddyassembly.CaddyNaiveUser {
	t.Helper()
	plan, _, _, err := caddyassembly.BuildFinalRenderPlan(settings, []model.Inbound{inbound})
	if err != nil {
		t.Fatalf("BuildFinalRenderPlan: %v", err)
	}
	server, ok := plan.Servers[bindregistry.BindKey{Address: "0.0.0.0", Port: 443, Network: bindregistry.ListenTCP}]
	if !ok || server.Kind != caddyassembly.CaddyOwnerNaive {
		t.Fatalf("no naive server on tcp :443: %+v", plan.Servers)
	}
	if got := caddyassembly.NaiveUsers(inbound, settings); !sameNaiveUsers(got, server.NaiveUsers) {
		t.Fatalf("plan users %+v differ from NaiveUsers %+v", server.NaiveUsers, got)
	}
	return server.NaiveUsers
}

func sameNaiveUsers(a, b []caddyassembly.CaddyNaiveUser) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestBuildRenderPlanNaiveRevokedSentinelIsSecretKeyed pins the plan-level
// revoked-client sentinel (#1098): a credential-managed naive inbound with no
// usable credential must render exactly one never-authenticating account,
// keyed by the per-install secret, and must never revive the inbound-level
// fallback credential or render zero users (an open relay).
func TestBuildRenderPlanNaiveRevokedSentinelIsSecretKeyed(t *testing.T) {
	settings := model.Settings{
		PanelAccess:                "direct",
		DefaultInboundPublicPort:   443,
		CredentialDerivationSecret: "install-secret-a",
		DefaultAcmeEmail:           "admin@vpn.example.com",
	}
	cases := map[string]model.Inbound{
		"all_profiles_disabled": naiveInbound("naive", "vpn.example.com", 443, "tcp",
			model.ClientProfile{Name: "c1", Username: "alice", Password: "alice-pass", Enabled: false}),
		"bindings_all_revoked": func() model.Inbound {
			in := naiveInbound("naive", "vpn.example.com", 443, "tcp")
			in.HasClientBindings = true
			return in
		}(),
		"legacy_profiles_suppressed": func() model.Inbound {
			in := naiveInbound("naive", "vpn.example.com", 443, "tcp")
			in.LegacyProfilesSuppressed = true
			return in
		}(),
	}
	for name, inbound := range cases {
		t.Run(name, func(t *testing.T) {
			// A live inbound-level fallback credential must not be revived.
			inbound.NaiveUsername = "fallback-user"
			inbound.NaivePassword = "fallback-pass"

			users := planNaiveUsers(t, settings, inbound)
			wantUser, wantPass := model.RevokedClientCredential(settings, inbound)
			if len(users) != 1 || users[0].Username != wantUser || users[0].Password != wantPass {
				t.Fatalf("users = %+v, want only the keyed sentinel %q", users, wantUser)
			}
			if wantUser != "veil-revoked-naive" {
				t.Fatalf("sentinel username = %q, want veil-revoked-naive", wantUser)
			}
			if users[0].Password == "fallback-pass" || users[0].Username == "fallback-user" {
				t.Fatalf("revoked inbound revived the fallback credential: %+v", users)
			}

			// Deterministic across renders (no config churn) ...
			if again := planNaiveUsers(t, settings, inbound); !sameNaiveUsers(users, again) {
				t.Fatalf("sentinel not stable across renders: %+v vs %+v", users, again)
			}
			// ... and keyed by the install secret, so it is not computable
			// from the inbound alone.
			other := settings
			other.CredentialDerivationSecret = "install-secret-b"
			if otherUsers := planNaiveUsers(t, other, inbound); len(otherUsers) != 1 || otherUsers[0].Password == users[0].Password {
				t.Fatalf("sentinel password ignores the install secret: %+v vs %+v", otherUsers, users)
			}
		})
	}
}

// TestBuildRenderPlanNaiveUnmanagedInboundKeepsFallback guards the other side
// of the HadClientProfiles gate: an inbound that never had client profiles or
// bindings still renders its inbound-level credential, not the sentinel.
func TestBuildRenderPlanNaiveUnmanagedInboundKeepsFallback(t *testing.T) {
	settings := model.Settings{PanelAccess: "direct", DefaultInboundPublicPort: 443, CredentialDerivationSecret: "install-secret-a", DefaultAcmeEmail: "admin@vpn.example.com"}
	inbound := naiveInbound("naive", "vpn.example.com", 443, "tcp")
	inbound.NaiveUsername = "fallback-user"
	inbound.NaivePassword = "fallback-pass"

	users := planNaiveUsers(t, settings, inbound)
	if len(users) != 1 || users[0] != (caddyassembly.CaddyNaiveUser{Username: "fallback-user", Password: "fallback-pass"}) {
		t.Fatalf("users = %+v, want the inbound fallback credential", users)
	}
}
