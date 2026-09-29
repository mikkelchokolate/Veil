package api

import (
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/client"
)

func TestHysteria2TrafficIdentityMapAliasesLegacyUsernameAfterRename(t *testing.T) {
	const bindingID = "fb9c7f5b-37b0-4e3d-8c76-69cc911b19c7"
	const runtime = "v_fb9c7f5b37b04e3d8c7669cc911b19c7"
	clientID := client.StableClientID("sfhgs", "client_gg3iemj")
	identities := trafficIdentityMap(
		"sfhgs",
		[]ClientProfile{{Name: "client_gg3iemj", Username: "client_gg3iemj", Enabled: true}},
		[]client.Binding{{
			ID: bindingID, ClientID: clientID, InboundID: "sfhgs",
			RuntimeIdentity: runtime, Enabled: true,
		}},
		[]client.Client{{ID: clientID, Name: "phone", Enabled: true}},
		time.Now().Unix(),
	)
	if identities[runtime] != bindingID {
		t.Fatalf("canonical runtime identity missing: %#v", identities)
	}
	if identities["client_gg3iemj"] != bindingID {
		t.Fatalf("legacy username not aliased after rename: %#v", identities)
	}
	if identities["phone"] != bindingID {
		t.Fatalf("client name not aliased: %#v", identities)
	}
}

func TestHysteria2TrafficIdentityMapDoesNotOverrideCanonicalIdentity(t *testing.T) {
	identities := trafficIdentityMap(
		"hy",
		[]ClientProfile{{Username: "alice", Enabled: true}},
		[]client.Binding{
			{ID: "bind-a", ClientID: "client-a", InboundID: "hy", RuntimeIdentity: "alice", Enabled: true},
			{ID: "bind-b", ClientID: "client-b", InboundID: "hy", RuntimeIdentity: "v_other", Enabled: true},
		},
		[]client.Client{
			{ID: "client-a", Name: "carol", Enabled: true},
			{ID: "client-b", Name: "alice", Enabled: true},
		},
		time.Now().Unix(),
	)
	if identities["alice"] != "bind-a" {
		t.Fatalf("client name overwrote a canonical runtime identity: %#v", identities)
	}
}

func TestHysteria2TrafficIdentityMapIgnoresForeignProfile(t *testing.T) {
	identities := trafficIdentityMap(
		"hy",
		[]ClientProfile{{Username: "stranger", Enabled: true}},
		[]client.Binding{{ID: "bind-a", ClientID: "client-a", InboundID: "hy", RuntimeIdentity: "v_a", Enabled: true}},
		[]client.Client{{ID: "client-a", Name: "alice", Enabled: true}},
		time.Now().Unix(),
	)
	if _, ok := identities["stranger"]; ok {
		t.Fatalf("unrelated profile username was mapped: %#v", identities)
	}
}

// TestTrafficIdentityMapSkipsRenderExcludedClients (#1177): a binding whose
// client the render path already excludes — disabled, quota-depleted,
// expired, or dangling — carries only residual telemetry, so none of its
// identities (runtime identity, client name, legacy alias) may attribute.
func TestTrafficIdentityMapSkipsRenderExcludedClients(t *testing.T) {
	now := time.Now().Unix()
	past := now - 60
	future := now + 3600
	bindings := []client.Binding{
		{ID: "bind-disabled", ClientID: "c-disabled", InboundID: "hy", RuntimeIdentity: "v_disabled", Enabled: true},
		{ID: "bind-depleted", ClientID: "c-depleted", InboundID: "hy", RuntimeIdentity: "v_depleted", Enabled: true},
		{ID: "bind-expired", ClientID: "c-expired", InboundID: "hy", RuntimeIdentity: "v_expired", Enabled: true},
		{ID: "bind-missing", ClientID: "c-missing", InboundID: "hy", RuntimeIdentity: "v_missing", Enabled: true},
		{ID: "bind-live", ClientID: "c-live", InboundID: "hy", RuntimeIdentity: "v_live", Enabled: true},
	}
	clients := []client.Client{
		{ID: "c-disabled", Name: "disabled-name", Enabled: false},
		{ID: "c-depleted", Name: "depleted-name", Enabled: true, Depleted: true},
		{ID: "c-expired", Name: "expired-name", Enabled: true, ExpiresAt: &past},
		{ID: "c-live", Name: "live-name", Enabled: true, ExpiresAt: &future},
	}
	identities := trafficIdentityMap("hy", nil, bindings, clients, now)

	if identities["v_live"] != "bind-live" || identities["live-name"] != "bind-live" {
		t.Fatalf("eligible client identities missing: %#v", identities)
	}
	for _, identity := range []string{"v_disabled", "disabled-name", "v_depleted", "depleted-name", "v_expired", "expired-name", "v_missing"} {
		if _, ok := identities[identity]; ok {
			t.Fatalf("identity %q attributed for an excluded client: %#v", identity, identities)
		}
	}
	if len(identities) != 2 {
		t.Fatalf("expected exactly the live client's 2 identities, got %#v", identities)
	}
}
