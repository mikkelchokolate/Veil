package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestPublicSubscriptionExpiresAtUsesRequestClock(t *testing.T) {
	router, state := newSubscriptionTestRouter(t)
	inbound := v1Request(t, router, http.MethodPost, "/api/inbounds",
		`{"name":"hy2-expired-feed","protocol":"hysteria2","transport":"udp","port":27445,"enabled":true}`)
	if inbound.Code != http.StatusCreated && inbound.Code != http.StatusOK {
		t.Fatalf("inbound: %d %s", inbound.Code, inbound.Body.String())
	}
	created := v1Request(t, router, http.MethodPost, "/api/v1/clients",
		`{"name":"expired-feed","expiresAt":200,"bindings":[{"inboundId":"hy2-expired-feed","credential":"pw-expired-feed"}]}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("client: %d %s", created.Code, created.Body.String())
	}
	clientID := unwrapClient(t, created.Body.Bytes())["id"].(string)
	issued, err := state.tokenStore.Issue(clientID, "expired-feed", nil)
	if err != nil {
		t.Fatal(err)
	}

	revisions, err := state.applyRevisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	if revisions.Applied == 0 {
		t.Fatal("expected applied revision")
	}
	payload, err := state.applySnapshots.Load(revisions.Applied)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot model.ManagementSnapshot
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		t.Fatal(err)
	}
	snapshot.EffectiveAt = 100
	found := false
	for i := range snapshot.Clients {
		if snapshot.Clients[i].ID != clientID {
			continue
		}
		if snapshot.Clients[i].ExpiresAt == nil || *snapshot.Clients[i].ExpiresAt != 200 {
			t.Fatalf("snapshot expiresAt = %v, want 200", snapshot.Clients[i].ExpiresAt)
		}
		found = true
	}
	if !found {
		t.Fatal("applied snapshot missing expired client")
	}
	payload, err = json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.db.Exec(`UPDATE revision_snapshots SET payload=? WHERE revision=?`, payload, revisions.Applied); err != nil {
		t.Fatal(err)
	}
	state.appliedProjectionMu.Lock()
	state.appliedProjections = nil
	state.appliedProjectionRevision = 0
	state.appliedProjectionMu.Unlock()

	after := publicRawSubscription(t, router, issued.Plaintext)
	if after.Code != http.StatusOK {
		t.Fatalf("subscription: %d %s", after.Code, after.Body.String())
	}
	if strings.Contains(after.Body.String(), "pw-expired-feed") {
		t.Fatalf("expired client still received credentials: %q", after.Body.String())
	}

	detail := v1Request(t, router, http.MethodGet, "/api/v1/clients/"+clientID, "")
	if detail.Code != http.StatusOK {
		t.Fatalf("client get: %d %s", detail.Code, detail.Body.String())
	}
	view := unwrapClient(t, detail.Body.Bytes())
	if view["status"] != "expired" {
		t.Fatalf("client status = %v, want expired", view["status"])
	}
}
