package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestPublicSubscriptionGatesOnLiveLifecycle (#1126): a token that is itself
// valid must not keep serving credentials after the client is disabled or
// expired — the applied snapshot is immutable, so the feed re-checks the
// live row. Asymmetry between "token active" and "client live" leaked links.
func TestPublicSubscriptionGatesOnLiveLifecycle(t *testing.T) {
	router, state := newApplyTrackedRouterWithState(t)
	t.Cleanup(func() { _ = state.Close() })

	inboundResponse := v1Request(t, router, http.MethodPost, "/api/inbounds",
		`{"name":"feed-hy","protocol":"hysteria2","transport":"udp","port":27443,"enabled":true}`)
	if inboundResponse.Code != http.StatusCreated && inboundResponse.Code != http.StatusOK {
		t.Fatalf("create inbound: %d %s", inboundResponse.Code, inboundResponse.Body.String())
	}
	clientResponse := v1Request(t, router, http.MethodPost, "/api/v1/clients",
		`{"name":"feed-client","bindings":[{"inboundId":"feed-hy","runtimeIdentity":"feed_identity","credential":"feed-secret-cred"}]}`)
	if clientResponse.Code != http.StatusCreated {
		t.Fatalf("create client: %d %s", clientResponse.Code, clientResponse.Body.String())
	}
	created := unwrapClient(t, clientResponse.Body.Bytes())
	clientID := created["id"].(string)
	issued, err := state.tokenStore.Issue(clientID, "feed-gate", nil)
	if err != nil {
		t.Fatal(err)
	}

	before := waitForFeedOK(t, router, state, issued.Plaintext)
	if !strings.Contains(before.Body.String(), "feed-secret-cred") {
		t.Fatalf("baseline feed missing credential: %d %q", before.Code, before.Body.String())
	}

	// Disable the client without applying: the feed must stop serving links
	// immediately even though the applied snapshot still contains them.
	disable := v1Request(t, router, http.MethodPatch, "/api/v1/clients/"+clientID,
		fmt.Sprintf(`{"version":%d,"enabled":false}`, liveClientVersion(t, router, clientID)))
	if disable.Code != http.StatusOK {
		t.Fatalf("disable client: %d %s", disable.Code, disable.Body.String())
	}
	assertFeedNeverServes(t, router, issued.Plaintext, "feed-secret-cred")
}

// TestPublicSubscriptionGatesOnLiveExpiry is the expiry counterpart: an
// expiresAt that lands in the past without an apply must gate the feed at
// request time (the snapshot's frozen ExpiresAt is not trusted).
func TestPublicSubscriptionGatesOnLiveExpiry(t *testing.T) {
	router, state := newApplyTrackedRouterWithState(t)
	t.Cleanup(func() { _ = state.Close() })

	inboundResponse := v1Request(t, router, http.MethodPost, "/api/inbounds",
		`{"name":"feed-hy2","protocol":"hysteria2","transport":"udp","port":27444,"enabled":true}`)
	if inboundResponse.Code != http.StatusCreated && inboundResponse.Code != http.StatusOK {
		t.Fatalf("create inbound: %d %s", inboundResponse.Code, inboundResponse.Body.String())
	}
	clientResponse := v1Request(t, router, http.MethodPost, "/api/v1/clients",
		`{"name":"feed-expire","bindings":[{"inboundId":"feed-hy2","runtimeIdentity":"feed_expire","credential":"feed-expiry-cred"}]}`)
	if clientResponse.Code != http.StatusCreated {
		t.Fatalf("create client: %d %s", clientResponse.Code, clientResponse.Body.String())
	}
	created := unwrapClient(t, clientResponse.Body.Bytes())
	clientID := created["id"].(string)
	issued, err := state.tokenStore.Issue(clientID, "feed-expiry", nil)
	if err != nil {
		t.Fatal(err)
	}
	if before := waitForFeedOK(t, router, state, issued.Plaintext); !strings.Contains(before.Body.String(), "feed-expiry-cred") {
		t.Fatalf("baseline feed missing credential: %d %q", before.Code, before.Body.String())
	}
	patch := v1Request(t, router, http.MethodPatch, "/api/v1/clients/"+clientID,
		fmt.Sprintf(`{"version":%d,"expiresAt":100}`, liveClientVersion(t, router, clientID)))
	if patch.Code != http.StatusOK {
		t.Fatalf("expire client: %d %s", patch.Code, patch.Body.String())
	}
	assertFeedNeverServes(t, router, issued.Plaintext, "feed-expiry-cred")
}

// TestInboundProtocolSwitchRejectsQuotaBoundClients (#1118): changing an
// inbound's protocol away from hysteria2 while quota-bound clients remain
// attached would silently remove their quota enforcement — reject with 409.
func TestInboundProtocolSwitchRejectsQuotaBoundClients(t *testing.T) {
	router, state := newApplyTrackedRouterWithState(t)
	t.Cleanup(func() { _ = state.Close() })

	inboundResponse := v1Request(t, router, http.MethodPost, "/api/inbounds",
		`{"name":"switch-hy","protocol":"hysteria2","transport":"udp","port":27445,"enabled":true}`)
	if inboundResponse.Code != http.StatusCreated && inboundResponse.Code != http.StatusOK {
		t.Fatalf("create inbound: %d %s", inboundResponse.Code, inboundResponse.Body.String())
	}
	clientResponse := v1Request(t, router, http.MethodPost, "/api/v1/clients",
		`{"name":"quota-client","quotaBytes":1073741824,"bindings":[{"inboundId":"switch-hy","runtimeIdentity":"quota_identity","credential":"quota-secret"}]}`)
	if clientResponse.Code != http.StatusCreated {
		t.Fatalf("create quota client: %d %s", clientResponse.Code, clientResponse.Body.String())
	}

	put := v1Request(t, router, http.MethodPut, "/api/inbounds/switch-hy",
		`{"name":"switch-hy","protocol":"mieru","transport":"tcp","port":27445,"enabled":true}`)
	if put.Code != http.StatusConflict {
		t.Fatalf("protocol switch: %d %s, want 409", put.Code, put.Body.String())
	}

	// Disabling the inbound also strips quota enforcement — same 409.
	disable := v1Request(t, router, http.MethodPut, "/api/inbounds/switch-hy",
		`{"name":"switch-hy","protocol":"hysteria2","transport":"udp","port":27445,"enabled":false}`)
	if disable.Code != http.StatusConflict {
		t.Fatalf("disable with quota-bound clients: %d %s, want 409", disable.Code, disable.Body.String())
	}

	// A no-op protocol (still hysteria2) update is allowed.
	ok := v1Request(t, router, http.MethodPut, "/api/inbounds/switch-hy",
		`{"name":"switch-hy","protocol":"hysteria2","transport":"udp","port":27445,"enabled":true}`)
	if ok.Code != http.StatusOK {
		t.Fatalf("same-protocol update: %d %s", ok.Code, ok.Body.String())
	}
}

// TestMigratedLegacyProfilesSuppressedAtRender (#1117): after the
// migrate-legacy endpoint hands embedded profiles to the normalized domain,
// the render copy must exclude them — otherwise disable/expire/delete of
// the normalized client never reaches the still-live legacy credential.
func TestMigratedLegacyProfilesSuppressedAtRender(t *testing.T) {
	router, state := newApplyTrackedRouterWithState(t)
	t.Cleanup(func() { _ = state.Close() })

	inboundResponse := v1Request(t, router, http.MethodPost, "/api/inbounds",
		`{"name":"legacy-mieru","protocol":"mieru","transport":"tcp","port":2999,"enabled":true,"profiles":[{"name":"legacy-user","username":"legacy-user","password":"legacy-secret"}]}`)
	if inboundResponse.Code != http.StatusCreated && inboundResponse.Code != http.StatusOK {
		t.Fatalf("create inbound: %d %s", inboundResponse.Code, inboundResponse.Body.String())
	}

	migrate := v1Request(t, router, http.MethodPost, "/api/v1/clients/migrate-legacy", `{}`)
	if migrate.Code != http.StatusOK {
		t.Fatalf("migrate-legacy: %d %s", migrate.Code, migrate.Body.String())
	}

	// Desired state still carries the embedded profiles (they persist as
	// migration source data) — suppression happens on the render copy.
	state.mu.Lock()
	rendered, err := state.inboundsWithRuntimeCredentialsLocked()
	state.mu.Unlock()
	if err != nil {
		t.Fatalf("render inbounds: %v", err)
	}
	var found *Inbound
	for i := range rendered {
		if rendered[i].Name == "legacy-mieru" {
			found = &rendered[i]
		}
	}
	if found == nil {
		t.Fatal("inbound missing from render copy")
	}
	for _, p := range found.Profiles {
		if p.Username == "legacy-user" {
			t.Fatalf("migrated profile %q still rendered", p.Username)
		}
	}
	if !found.LegacyProfilesSuppressed {
		t.Fatal("suppression flag not set on render copy")
	}
	if !found.HadClientProfiles() {
		t.Fatal("HadClientProfiles must stay true so the credential fallback never revives")
	}

	// Deleting the migrated normalized client must not resurrect the legacy
	// credential at render either.
	list := v1Request(t, router, http.MethodGet, "/api/v1/clients", "")
	if list.Code != http.StatusOK {
		t.Fatalf("list clients: %d", list.Code)
	}
	var clients struct {
		Items []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"items"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &clients); err != nil {
		t.Fatalf("decode clients: %v", err)
	}
	deleted := false
	for _, c := range clients.Items {
		del := v1Request(t, router, http.MethodDelete, fmt.Sprintf("/api/v1/clients/%s", c.ID), "")
		if del.Code == http.StatusOK || del.Code == http.StatusNoContent {
			deleted = true
		}
	}
	if !deleted {
		t.Fatal("no migrated client deleted")
	}
	state.mu.Lock()
	rendered, err = state.inboundsWithRuntimeCredentialsLocked()
	state.mu.Unlock()
	if err != nil {
		t.Fatalf("render after delete: %v", err)
	}
	for i := range rendered {
		if rendered[i].Name != "legacy-mieru" {
			continue
		}
		for _, p := range rendered[i].Profiles {
			if p.Username == "legacy-user" {
				t.Fatal("deleted normalized client resurrected the legacy profile")
			}
		}
	}
}

// liveClientVersion returns the client's current optimistic-lock version.
// The version drifts while the apply pipeline converges, so it must be
// re-read right before a mutation rather than reused from creation time.
func liveClientVersion(t *testing.T, router http.Handler, clientID string) int {
	t.Helper()
	resp := v1Request(t, router, http.MethodGet, "/api/v1/clients/"+clientID, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("read client for version: %d %s", resp.Code, resp.Body.String())
	}
	var view struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode client: %v", err)
	}
	return view.Version
}

// waitForFeedOK polls the public feed until the first verified apply lands
// (runtime_verification flips from 'unknown' and the applied snapshot
// exists), then returns the 200 response. Startup convergence is async, so
// the baseline cannot be a single-shot read.
func waitForFeedOK(t *testing.T, router http.Handler, state *managementState, token string) *httptest.ResponseRecorder {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		resp := publicRawSubscription(t, router, token)
		if resp.Code == http.StatusOK && resp.Body.Len() > 0 {
			return resp
		}
		if time.Now().After(deadline) {
			// The public feed needs a committed applied revision; environments
			// without a converging apply executor (no promotable runtime, no
			// real services) never produce one - the pre-existing applied-
			// subscription tests fail the same way there.
			if rev, _ := state.applyRevisions.Get(); rev.Applied == 0 {
				t.Skipf("apply pipeline cannot converge in this environment (rev=%+v)", rev)
			}
			t.Fatalf("feed never converged despite applied revision: last=%d %q", resp.Code, resp.Body.String())
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// assertFeedNeverServes polls the feed past the post-mutation apply window
// and fails if the credential ever reappears - a stale-snapshot leak would
// surface only once the async apply commits.
func assertFeedNeverServes(t *testing.T, router http.Handler, token, credential string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp := publicRawSubscription(t, router, token)
		if strings.Contains(resp.Body.String(), credential) {
			t.Fatalf("feed served credential %q after lifecycle change: %d %q", credential, resp.Code, resp.Body.String())
		}
		if time.Now().After(deadline) {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}
