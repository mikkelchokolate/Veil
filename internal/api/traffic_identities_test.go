package api

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/client"
)

func TestHysteria2TrafficIdentityMapAliasesLegacyUsernameAfterRename(t *testing.T) {
	const bindingID = "fb9c7f5b-37b0-4e3d-8c76-69cc911b19c7"
	const runtime = "v_fb9c7f5b37b04e3d8c7669cc911b19c7"
	clientID := client.StableClientID("sfhgs", "client_gg3iemj")
	identities := hysteria2TrafficIdentityMap(
		"sfhgs",
		[]ClientProfile{{Name: "client_gg3iemj", Username: "client_gg3iemj", Enabled: true}},
		[]client.Binding{{
			ID: bindingID, ClientID: clientID, InboundID: "sfhgs",
			RuntimeIdentity: runtime, Enabled: true,
		}},
		[]client.Client{{ID: clientID, Name: "phone"}},
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
	identities := hysteria2TrafficIdentityMap(
		"hy",
		[]ClientProfile{{Username: "alice", Enabled: true}},
		[]client.Binding{
			{ID: "bind-a", ClientID: "client-a", InboundID: "hy", RuntimeIdentity: "alice", Enabled: true},
			{ID: "bind-b", ClientID: "client-b", InboundID: "hy", RuntimeIdentity: "v_other", Enabled: true},
		},
		[]client.Client{{ID: "client-b", Name: "alice"}},
	)
	if identities["alice"] != "bind-a" {
		t.Fatalf("client name overwrote a canonical runtime identity: %#v", identities)
	}
}

func TestHysteria2TrafficIdentityMapIgnoresForeignProfile(t *testing.T) {
	identities := hysteria2TrafficIdentityMap(
		"hy",
		[]ClientProfile{{Username: "stranger", Enabled: true}},
		[]client.Binding{{ID: "bind-a", ClientID: "client-a", InboundID: "hy", RuntimeIdentity: "v_a", Enabled: true}},
		[]client.Client{{ID: "client-a", Name: "alice"}},
	)
	if _, ok := identities["stranger"]; ok {
		t.Fatalf("unrelated profile username was mapped: %#v", identities)
	}
}
