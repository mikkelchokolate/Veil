package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestSubscriptionHEADPerformsNoTelemetryWriteOrArtifactRendering(t *testing.T) {
	r, state := newApplyTrackedRouterWithState(t)
	plaintext, _ := seedClientWithToken(t, r)

	var before int64
	if err := state.db.QueryRow(`SELECT COALESCE(last_used_at,0) FROM subscription_tokens WHERE token_hash IS NOT NULL LIMIT 1`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := state.db.Exec(`PRAGMA query_only=ON`); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodHead, "/s/"+plaintext, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("HEAD status=%d body=%s", w.Code, w.Body.String())
	}
	if w.Body.Len() != 0 {
		t.Fatalf("HEAD returned body %q", w.Body.String())
	}
	if _, err := state.db.Exec(`PRAGMA query_only=OFF`); err != nil {
		t.Fatal(err)
	}
	var after int64
	if err := state.db.QueryRow(`SELECT COALESCE(last_used_at,0) FROM subscription_tokens WHERE token_hash IS NOT NULL LIMIT 1`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("HEAD wrote token telemetry: before=%d after=%d", before, after)
	}

	badPath := httptest.NewRequest(http.MethodGet, "/s/"+plaintext+"/extra", nil)
	bad := httptest.NewRecorder()
	r.ServeHTTP(bad, badPath)
	if bad.Code != http.StatusNotFound {
		t.Fatalf("trailing subscription segment status=%d", bad.Code)
	}
}

func TestSubscriptionIgnoresAnotherClientsCorruptCredential(t *testing.T) {
	r, state := newApplyTrackedRouterWithState(t)
	firstToken, firstID := seedClientWithToken(t, r)
	secondResponse := v1Request(t, r, http.MethodPost, "/api/v1/clients", `{"name":"second-subscription-client","bindings":[{"inboundId":"hy2-sub","credential":"second-password"}]}`)
	if secondResponse.Code != http.StatusCreated {
		t.Fatalf("seed second client: %d %s", secondResponse.Code, secondResponse.Body.String())
	}
	var secondBody map[string]any
	if err := json.NewDecoder(secondResponse.Body).Decode(&secondBody); err != nil {
		t.Fatal(err)
	}
	if nested, ok := secondBody["client"].(map[string]any); ok {
		secondBody = nested
	}
	secondID, _ := secondBody["id"].(string)
	revisions, err := state.applyRevisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := state.applySnapshots.Load(revisions.Applied)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot model.ManagementSnapshot
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		t.Fatal(err)
	}
	secondBindings := make(map[string]struct{})
	for _, binding := range snapshot.Bindings {
		if binding.ClientID == secondID {
			secondBindings[binding.ID] = struct{}{}
		}
	}
	corrupted := false
	for index := range snapshot.Credentials {
		if _, ok := secondBindings[snapshot.Credentials[index].BindingID]; ok {
			snapshot.Credentials[index].EncryptedValue = []byte("not-valid-ciphertext")
			corrupted = true
		}
	}
	if !corrupted || firstID == secondID {
		t.Fatal("test did not find isolated second-client credential")
	}
	payload, err = json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.db.Exec(`UPDATE revision_snapshots SET payload=? WHERE revision=?`, payload, revisions.Applied); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/s/"+firstToken, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("unrelated corrupt credential broke subscription: status=%d body=%s", w.Code, w.Body.String())
	}
}
