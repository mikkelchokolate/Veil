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

	req := httptest.NewRequest(http.MethodHead, "/s/"+plaintext, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("HEAD status=%d body=%s", w.Code, w.Body.String())
	}
	if w.Body.Len() != 0 {
		t.Fatalf("HEAD returned body %q", w.Body.String())
	}
	if w.Header().Get("Subscription-Userinfo") == "" || w.Header().Get("X-Veil-Applied-Revision") == "" {
		t.Fatalf("HEAD omitted subscription metadata: %v", w.Header())
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

func TestPublicSubscriptionHEADMatchesGETMetadata(t *testing.T) {
	router, _ := newSubscriptionTestRouter(t)
	plaintext, _ := seedClientWithToken(t, router)

	getReq := httptest.NewRequest(http.MethodGet, "/s/"+plaintext+"?format=raw", nil)
	get := httptest.NewRecorder()
	router.ServeHTTP(get, getReq)
	if get.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", get.Code, get.Body.String())
	}

	headReq := httptest.NewRequest(http.MethodHead, "/s/"+plaintext+"?format=raw", nil)
	head := httptest.NewRecorder()
	router.ServeHTTP(head, headReq)
	if head.Code != http.StatusOK {
		t.Fatalf("HEAD status=%d body=%s", head.Code, head.Body.String())
	}
	if head.Body.Len() != 0 {
		t.Fatalf("HEAD returned body %q", head.Body.String())
	}
	for _, key := range []string{
		"Subscription-Userinfo",
		"Profile-Title",
		"Profile-Update-Interval",
		"X-Veil-Configuration-State",
		"X-Veil-Applied-Revision",
		"X-Veil-Desired-Revision",
		"Content-Type",
		"Content-Disposition",
	} {
		if got, want := head.Header().Get(key), get.Header().Get(key); got != want || got == "" {
			t.Fatalf("HEAD %s = %q, GET %s = %q", key, got, key, want)
		}
	}
}

func TestPublicSubscriptionHEADRejectsInvalidFormat(t *testing.T) {
	router, _ := newSubscriptionTestRouter(t)
	plaintext, _ := seedClientWithToken(t, router)

	getReq := httptest.NewRequest(http.MethodGet, "/s/"+plaintext+"?format=unsupported", nil)
	get := httptest.NewRecorder()
	router.ServeHTTP(get, getReq)
	if get.Code != http.StatusBadRequest {
		t.Fatalf("GET invalid format status=%d body=%s", get.Code, get.Body.String())
	}

	headReq := httptest.NewRequest(http.MethodHead, "/s/"+plaintext+"?format=unsupported", nil)
	head := httptest.NewRecorder()
	router.ServeHTTP(head, headReq)
	if head.Code != http.StatusBadRequest {
		t.Fatalf("HEAD invalid format status=%d body=%s", head.Code, head.Body.String())
	}
	if head.Body.Len() != 0 {
		t.Fatalf("HEAD invalid format returned body %q", head.Body.String())
	}
}

func TestPublicSubscriptionHEADUnavailableWithoutAppliedRevision(t *testing.T) {
	router, state := newSubscriptionTestRouter(t)
	plaintext, _ := seedClientWithToken(t, router)
	if _, err := state.db.Exec(`UPDATE revisions SET applied_revision=0`); err != nil {
		t.Fatal(err)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/s/"+plaintext+"?format=raw", nil)
	get := httptest.NewRecorder()
	router.ServeHTTP(get, getReq)
	if get.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET missing applied revision status=%d body=%s", get.Code, get.Body.String())
	}

	headReq := httptest.NewRequest(http.MethodHead, "/s/"+plaintext+"?format=raw", nil)
	head := httptest.NewRecorder()
	router.ServeHTTP(head, headReq)
	if head.Code != http.StatusServiceUnavailable {
		t.Fatalf("HEAD missing applied revision status=%d body=%s", head.Code, head.Body.String())
	}
	if head.Body.Len() != 0 {
		t.Fatalf("HEAD missing applied revision returned body %q", head.Body.String())
	}
}

func TestPublicSubscriptionHEADHTMLHasNoBody(t *testing.T) {
	router, _ := newSubscriptionTestRouter(t)
	plaintext, _ := seedClientWithToken(t, router)

	req := httptest.NewRequest(http.MethodHead, "/s/"+plaintext, nil)
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Sec-Fetch-Dest", "document")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("HEAD html status=%d body=%s", w.Code, w.Body.String())
	}
	if w.Body.Len() != 0 {
		t.Fatalf("HEAD html returned body %q", w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Fatalf("HEAD html content-type = %q", ct)
	}
}
