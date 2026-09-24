package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// issueTestToken issues a token through the real endpoint and returns its ID
// and one-time plaintext.
func issueTestToken(t *testing.T, r http.Handler, clientID, body string) (id, plaintext string) {
	t.Helper()
	resp := v1Request(t, r, http.MethodPost, "/api/v1/clients/"+clientID+"/tokens", body)
	if resp.Code != http.StatusCreated {
		t.Fatalf("issue token: %d %s", resp.Code, resp.Body.String())
	}
	var created struct {
		Plaintext string `json:"plaintext"`
		Token     struct {
			ID string `json:"id"`
		} `json:"token"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode issue response: %v", err)
	}
	return created.Token.ID, created.Plaintext
}

// TestSubscriptionTokenListOmitsURLForInactiveTokens is the #966 regression:
// the token list must not attach a usable subscription URL to tokens that can
// no longer authenticate — expired and disabled tokens included, not just
// revoked ones.
func TestSubscriptionTokenListOmitsURLForInactiveTokens(t *testing.T) {
	router, state := newSubscriptionTestRouter(t)
	_, clientID := seedClientWithToken(t, router)

	expiredID, _ := issueTestToken(t, router, clientID, `{"label":"expired"}`)
	disabledID, _ := issueTestToken(t, router, clientID, `{"label":"disabled"}`)
	activeID, _ := issueTestToken(t, router, clientID, `{"label":"active"}`)

	// The public API refuses past expiry, so the expired fixture is stamped
	// directly — the same row an operator gets when time passes.
	if _, err := state.db.Exec(`UPDATE subscription_tokens SET expires_at=? WHERE id=?`, time.Now().Unix()-60, expiredID); err != nil {
		t.Fatalf("expire token: %v", err)
	}
	if err := state.tokenStore.SetEnabled(disabledID, false); err != nil {
		t.Fatalf("disable token: %v", err)
	}

	listed := v1Request(t, router, http.MethodGet, "/api/v1/clients/"+clientID+"/tokens", "")
	if listed.Code != http.StatusOK {
		t.Fatalf("list: %d %s", listed.Code, listed.Body.String())
	}
	var list struct {
		Items []struct {
			ID  string `json:"id"`
			URL string `json:"url"`
		} `json:"items"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	urls := map[string]string{}
	for _, item := range list.Items {
		urls[item.ID] = item.URL
	}
	if got := urls[expiredID]; got != "" {
		t.Fatalf("expired token listed a usable URL %q", got)
	}
	if got := urls[disabledID]; got != "" {
		t.Fatalf("disabled token listed a usable URL %q", got)
	}
	if got := urls[activeID]; !strings.Contains(got, "/s/") {
		t.Fatalf("active token lost its URL: %q", got)
	}
}

// TestSubscriptionTokenRevealRejectsInactiveTokens locks the reveal endpoint
// to the same active-state rule: disabled and expired tokens return the same
// rejection revoked tokens get, with no URL in the body (#966).
func TestSubscriptionTokenRevealRejectsInactiveTokens(t *testing.T) {
	router, state := newSubscriptionTestRouter(t)
	_, clientID := seedClientWithToken(t, router)

	expiredID, _ := issueTestToken(t, router, clientID, `{"label":"expired"}`)
	disabledID, _ := issueTestToken(t, router, clientID, `{"label":"disabled"}`)
	activeID, _ := issueTestToken(t, router, clientID, `{"label":"active"}`)

	if _, err := state.db.Exec(`UPDATE subscription_tokens SET expires_at=? WHERE id=?`, time.Now().Unix()-60, expiredID); err != nil {
		t.Fatalf("expire token: %v", err)
	}
	if err := state.tokenStore.SetEnabled(disabledID, false); err != nil {
		t.Fatalf("disable token: %v", err)
	}

	for name, id := range map[string]string{"expired": expiredID, "disabled": disabledID} {
		resp := v1Request(t, router, http.MethodGet, "/api/v1/clients/"+clientID+"/tokens/"+id, "")
		if resp.Code != http.StatusNotFound {
			t.Fatalf("%s token reveal: got %d, want 404 (%s)", name, resp.Code, resp.Body.String())
		}
		if strings.Contains(resp.Body.String(), "/s/") {
			t.Fatalf("%s token reveal leaked a subscription URL: %s", name, resp.Body.String())
		}
	}

	resp := v1Request(t, router, http.MethodGet, "/api/v1/clients/"+clientID+"/tokens/"+activeID, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("active token reveal: %d %s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "/s/") {
		t.Fatalf("active token reveal lost the URL: %s", resp.Body.String())
	}
}
