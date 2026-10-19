package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/client"
	"github.com/mikkelchokolate/Veil/internal/protocols/hysteria2"
)

// hy2OnlineThroughProvider feeds identities through the production
// hysteria2 stats provider against a stub /online table — the exact path
// hy2ClientOnlineSessions (deviceLimit/ipLimit admission) and the traffic
// collector take — and returns the per-binding session counts.
func hy2OnlineThroughProvider(t *testing.T, identities map[string]string, online string) (map[string]int64, []string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/online" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(online))
	}))
	t.Cleanup(server.Close)
	provider := hysteria2.NewStatsProvider("hysteria2:hy", server.URL+"/traffic", identities)
	counts, unknown, err := provider.Online(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return counts, unknown
}

// TestHysteria2TrafficIdentityMapFoldsCasedMigratedUsername (#1282): a legacy
// profile with a cased username migrates to its StableClientID binding; the
// daemon reports that user lowercased, so the alias must live in the
// lowercased key space and still be derived from the stored (cased) username.
func TestHysteria2TrafficIdentityMapFoldsCasedMigratedUsername(t *testing.T) {
	migratedID := client.StableClientID("hy", "Alice")
	profiles := []ClientProfile{{Username: "Alice", Enabled: true}}
	bindings := []client.Binding{{ID: "bind-migrated", ClientID: migratedID, InboundID: "hy", RuntimeIdentity: "v_migrated", Enabled: true}}
	clients := []client.Client{{ID: migratedID, Name: "Phone", Enabled: true}}
	now := time.Now().Unix()

	identities := hysteria2TrafficIdentityMap("hy", profiles, bindings, clients, now)
	if identities["alice"] != "bind-migrated" || identities["phone"] != "bind-migrated" || identities["v_migrated"] != "bind-migrated" {
		t.Fatalf("migrated identities not in the folded key space: %#v", identities)
	}
	for key := range identities {
		if key != toLowerASCIIForTest(key) {
			t.Fatalf("hysteria2 identity map kept a cased key %q: %#v", key, identities)
		}
	}

	counts, unknown := hy2OnlineThroughProvider(t, identities, `{"alice":2,"phone":1}`)
	if counts["bind-migrated"] != 3 || len(unknown) != 0 {
		t.Fatalf("cased migrated sessions not counted: counts=%v unknown=%v", counts, unknown)
	}

	// mieru usernames are case-sensitive: the exact map must not fold.
	exact := trafficIdentityMap("hy", profiles, bindings, clients, now)
	if exact["Alice"] != "bind-migrated" || exact["Phone"] != "bind-migrated" {
		t.Fatalf("exact (mieru) identity map lost case: %#v", exact)
	}
	if _, ok := exact["alice"]; ok {
		t.Fatalf("exact (mieru) identity map folded a key: %#v", exact)
	}
}

// TestHysteria2CanonicalIdentityBeatsCaseOnlyNameAlias (#1282): binding A's
// canonical RuntimeIdentity is "alice"; another client is NAMED "Alice". With
// case-preserved keys both entries survived and the provider's sorted fold
// gave "alice" to the name alias ("Alice" sorts first), so A's live sessions
// were charged to B's deviceLimit/ipLimit and A's traffic to B's usage.
func TestHysteria2CanonicalIdentityBeatsCaseOnlyNameAlias(t *testing.T) {
	identities := hysteria2TrafficIdentityMap(
		"hy",
		nil,
		[]client.Binding{
			{ID: "bind-a", ClientID: "client-a", InboundID: "hy", RuntimeIdentity: "alice", Enabled: true},
			{ID: "bind-b", ClientID: "client-b", InboundID: "hy", RuntimeIdentity: "v_b", Enabled: true},
		},
		[]client.Client{
			{ID: "client-a", Name: "carol", Enabled: true},
			{ID: "client-b", Name: "Alice", Enabled: true},
		},
		time.Now().Unix(),
	)
	if identities["alice"] != "bind-a" {
		t.Fatalf("case-only client-name alias stole a canonical runtime identity: %#v", identities)
	}
	counts, unknown := hy2OnlineThroughProvider(t, identities, `{"alice":2,"v_b":1}`)
	if counts["bind-a"] != 2 || counts["bind-b"] != 1 || len(unknown) != 0 {
		t.Fatalf("sessions misattributed across a case-only collision: counts=%v unknown=%v", counts, unknown)
	}
}

// TestHysteria2MigratedUsernameBeatsCaseOnlyNameAlias (#1282, sibling of
// #1225): a migrated legacy username must keep its claim even when another
// client's name differs from it only by case.
func TestHysteria2MigratedUsernameBeatsCaseOnlyNameAlias(t *testing.T) {
	migratedID := client.StableClientID("hy", "alice")
	identities := hysteria2TrafficIdentityMap(
		"hy",
		[]ClientProfile{{Username: "alice", Enabled: true}},
		[]client.Binding{
			{ID: "bind-migrated", ClientID: migratedID, InboundID: "hy", RuntimeIdentity: "v_migrated", Enabled: true},
			{ID: "bind-squatter", ClientID: "client-squatter", InboundID: "hy", RuntimeIdentity: "v_squatter", Enabled: true},
		},
		[]client.Client{
			{ID: migratedID, Name: "renamed-away", Enabled: true},
			{ID: "client-squatter", Name: "Alice", Enabled: true},
		},
		time.Now().Unix(),
	)
	counts, unknown := hy2OnlineThroughProvider(t, identities, `{"alice":4}`)
	if counts["bind-migrated"] != 4 || counts["bind-squatter"] != 0 || len(unknown) != 0 {
		t.Fatalf("migrated username claimed by a case-only name squatter: identities=%#v counts=%v", identities, counts)
	}
}

// TestHysteria2TrafficIdentityMapCaseOnlyRuntimeIdentityCollision: two rows
// whose RuntimeIdentity differs only by case collapse to one daemon user; the
// earliest binding (AllBindings order) keeps the claim deterministically.
func TestHysteria2TrafficIdentityMapCaseOnlyRuntimeIdentityCollision(t *testing.T) {
	identities := hysteria2TrafficIdentityMap(
		"hy",
		nil,
		[]client.Binding{
			{ID: "bind-first", ClientID: "c1", InboundID: "hy", RuntimeIdentity: "bob", Enabled: true},
			{ID: "bind-second", ClientID: "c2", InboundID: "hy", RuntimeIdentity: "Bob", Enabled: true},
		},
		[]client.Client{{ID: "c1", Name: "c1", Enabled: true}, {ID: "c2", Name: "c2", Enabled: true}},
		time.Now().Unix(),
	)
	if identities["bob"] != "bind-first" {
		t.Fatalf("case-only runtime identity collision not resolved to the earliest binding: %#v", identities)
	}
}

func toLowerASCIIForTest(s string) string {
	out := []byte(s)
	for i, c := range out {
		if c >= 'A' && c <= 'Z' {
			out[i] = c + ('a' - 'A')
		}
	}
	return string(out)
}
