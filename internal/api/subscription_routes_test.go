package api

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newSubscriptionTestRouter(t *testing.T) (http.Handler, *managementState) {
	t.Helper()
	router, state := newApplyTrackedRouterWithState(t)
	t.Cleanup(func() { _ = state.Close() })
	return router, state
}

// seedClientWithToken creates a client + binding + credential + token and
// returns the plaintext token. The subscription endpoint then renders links.
func seedClientWithToken(t *testing.T, r http.Handler) (plaintext, clientID string) {
	t.Helper()
	inbound := v1Request(t, r, http.MethodPost, "/api/inbounds", `{"name":"hy2-sub","protocol":"hysteria2","transport":"udp","port":9443,"enabled":true}`)
	if inbound.Code != http.StatusCreated && inbound.Code != http.StatusOK {
		t.Fatalf("seed inbound: %d %s", inbound.Code, inbound.Body.String())
	}
	var inboundEnvelope struct {
		Success bool `json:"success"`
	}
	if err := json.Unmarshal(inbound.Body.Bytes(), &inboundEnvelope); err != nil || !inboundEnvelope.Success {
		t.Fatalf("seed inbound did not apply: decode=%v body=%s", err, inbound.Body.String())
	}
	w := v1Request(t, r, http.MethodPost, "/api/v1/clients", `{"name":"alice","bindings":[{"inboundId":"hy2-sub","credential":"pw-alice"}]}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("seed client: %d %s", w.Code, w.Body.String())
	}
	var c map[string]any
	if err := json.NewDecoder(w.Body).Decode(&c); err != nil {
		t.Fatalf("decode seeded client: %v", err)
	}
	if success, ok := c["success"].(bool); ok && !success {
		t.Fatalf("seed client did not apply: %s", w.Body.String())
	}
	// S2 nested the created client under "client"; tolerate both shapes.
	if nested, ok := c["client"].(map[string]any); ok {
		c = nested
	}
	clientID = c["id"].(string)
	wt := v1Request(t, r, http.MethodPost, "/api/v1/clients/"+clientID+"/tokens", `{"label":"phone"}`)
	if wt.Code != http.StatusCreated {
		t.Fatalf("seed token: %d %s", wt.Code, wt.Body.String())
	}
	var tok struct {
		Plaintext string `json:"plaintext"`
	}
	_ = json.NewDecoder(wt.Body).Decode(&tok)
	if tok.Plaintext == "" {
		t.Fatalf("expected one-time plaintext, got %s", wt.Body.String())
	}
	return tok.Plaintext, clientID
}

func TestPublicSubscriptionServesLinksByToken(t *testing.T) {
	r, _ := newSubscriptionTestRouter(t)
	plaintext, _ := seedClientWithToken(t, r)

	req := httptest.NewRequest(http.MethodGet, "/s/"+plaintext, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("subscription: %d %s", w.Code, w.Body.String())
	}
	// Default format is base64; decode and assert it carries the credential.
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(w.Body.String()))
	if err != nil {
		t.Fatalf("expected base64 body, got %q (%v)", w.Body.String(), err)
	}
	if !strings.Contains(string(decoded), "pw-alice") {
		t.Fatalf("subscription must carry the binding credential, got %q", decoded)
	}
	// Metadata headers for proxy clients.
	if w.Header().Get("Subscription-Userinfo") == "" {
		t.Fatalf("expected Subscription-Userinfo header")
	}
	if w.Header().Get("Profile-Update-Interval") == "" {
		t.Fatalf("expected Profile-Update-Interval header")
	}
}

func TestPublicSubscriptionRawFormat(t *testing.T) {
	r, _ := newSubscriptionTestRouter(t)
	plaintext, _ := seedClientWithToken(t, r)
	req := httptest.NewRequest(http.MethodGet, "/s/"+plaintext+"?format=raw", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("raw: %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "pw-alice") {
		t.Fatalf("raw body must be plaintext links, got %q", w.Body.String())
	}
}

func TestPublicSubscriptionUnknownToken404(t *testing.T) {
	r, _ := newSubscriptionTestRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/s/nonexistent-token", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown token, got %d", w.Code)
	}
}

func TestPublicSubscriptionRevokedToken404(t *testing.T) {
	r, _ := newSubscriptionTestRouter(t)
	plaintext, clientID := seedClientWithToken(t, r)
	// Find the token id.
	wl := v1Request(t, r, http.MethodGet, "/api/v1/clients/"+clientID+"/tokens", "")
	var list struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	_ = json.NewDecoder(wl.Body).Decode(&list)
	if len(list.Items) == 0 {
		t.Fatalf("expected token listed")
	}
	v1Request(t, r, http.MethodDelete, "/api/v1/clients/"+clientID+"/tokens/"+list.Items[0].ID, "")
	// Now the public link must be dead.
	req := httptest.NewRequest(http.MethodGet, "/s/"+plaintext, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after revoke, got %d", w.Code)
	}
}

func TestPublicSubscriptionDisabledClientEmpty(t *testing.T) {
	r, _ := newSubscriptionTestRouter(t)
	plaintext, clientID := seedClientWithToken(t, r)
	// Disable the client.
	v1Request(t, r, http.MethodPatch, "/api/v1/clients/"+clientID, `{"version":1,"name":"alice","enabled":false}`)
	req := httptest.NewRequest(http.MethodGet, "/s/"+plaintext+"?format=raw", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (empty) for disabled client, got %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "pw-alice") {
		t.Fatalf("disabled client subscription must be empty, got %q", w.Body.String())
	}
}

func TestTokenRotateIssuesNewInvalidatesOld(t *testing.T) {
	r, _ := newSubscriptionTestRouter(t)
	plaintext, clientID := seedClientWithToken(t, r)
	wl := v1Request(t, r, http.MethodGet, "/api/v1/clients/"+clientID+"/tokens", "")
	var list struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	_ = json.NewDecoder(wl.Body).Decode(&list)
	wr := v1Request(t, r, http.MethodPost, "/api/v1/clients/"+clientID+"/tokens/"+list.Items[0].ID+"/rotate", `{}`)
	if wr.Code != http.StatusOK {
		t.Fatalf("rotate: %d %s", wr.Code, wr.Body.String())
	}
	var rotated struct {
		Plaintext string `json:"plaintext"`
	}
	_ = json.NewDecoder(wr.Body).Decode(&rotated)
	if rotated.Plaintext == "" || rotated.Plaintext == plaintext {
		t.Fatalf("rotate must return a NEW plaintext once")
	}
	// Old dead.
	req := httptest.NewRequest(http.MethodGet, "/s/"+plaintext, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("old token must be dead after rotate, got %d", w.Code)
	}
	// New works.
	req2 := httptest.NewRequest(http.MethodGet, "/s/"+rotated.Plaintext, nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("new token must work after rotate, got %d", w2.Code)
	}
}

func TestPublicSubscriptionHTMLLanding(t *testing.T) {
	r, _ := newSubscriptionTestRouter(t)
	plaintext, _ := seedClientWithToken(t, r)
	req := httptest.NewRequest(http.MethodGet, "/s/"+plaintext, nil)
	req.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("html: %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("expected html, got %q", ct)
	}
	if !strings.Contains(w.Body.String(), "alice") {
		t.Fatalf("html landing should show the profile title, got %q", w.Body.String())
	}
}

func TestClientLinksAndTokenRevealStayAvailableAfterIssue(t *testing.T) {
	r, _ := newSubscriptionTestRouter(t)
	_, clientID := seedClientWithToken(t, r)

	links := v1Request(t, r, http.MethodGet, "/api/v1/clients/"+clientID+"/links", "")
	if links.Code != http.StatusOK {
		t.Fatalf("links: %d %s", links.Code, links.Body.String())
	}
	if !strings.Contains(links.Body.String(), "pw-alice") && !strings.Contains(links.Body.String(), "hysteria2") {
		t.Fatalf("expected connection URI in links body, got %s", links.Body.String())
	}

	listed := v1Request(t, r, http.MethodGet, "/api/v1/clients/"+clientID+"/tokens", "")
	if listed.Code != http.StatusOK {
		t.Fatalf("list tokens: %d %s", listed.Code, listed.Body.String())
	}
	var tokenList struct {
		Items []struct {
			ID        string `json:"id"`
			HasSecret bool   `json:"hasSecret"`
			URL       string `json:"url"`
		} `json:"items"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &tokenList); err != nil || len(tokenList.Items) != 1 {
		t.Fatalf("token list: %v body=%s", err, listed.Body.String())
	}
	if !tokenList.Items[0].HasSecret || !strings.Contains(tokenList.Items[0].URL, "/s/") {
		t.Fatalf("list must include a recoverable subscription URL after reload: %s", listed.Body.String())
	}
	revealed := v1Request(t, r, http.MethodGet, "/api/v1/clients/"+clientID+"/tokens/"+tokenList.Items[0].ID, "")
	if revealed.Code != http.StatusOK {
		t.Fatalf("reveal: %d %s", revealed.Code, revealed.Body.String())
	}
	if !strings.Contains(revealed.Body.String(), `"/s/`) && !strings.Contains(revealed.Body.String(), "/s/") {
		t.Fatalf("reveal should include subscription URL, got %s", revealed.Body.String())
	}
}
