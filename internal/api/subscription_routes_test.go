package api

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// seedClientWithToken creates a client + binding + credential + token and
// returns the plaintext token. The subscription endpoint then renders links.
func seedClientWithToken(t *testing.T, r http.Handler) (plaintext, clientID string) {
	t.Helper()
	v1Request(t, r, http.MethodPost, "/api/inbounds", `{"name":"hy2-sub","protocol":"hysteria2","transport":"udp","port":9443,"enabled":true,"protocolFields":{"domain":"vpn.example.com"}}`)
	w := v1Request(t, r, http.MethodPost, "/api/v1/clients", `{"name":"alice","bindings":[{"inboundId":"hy2-sub","credential":"pw-alice"}]}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("seed client: %d %s", w.Code, w.Body.String())
	}
	var c map[string]any
	_ = json.NewDecoder(w.Body).Decode(&c)
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
	r, _ := newApplyTrackedRouter(t)
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
	r, _ := newApplyTrackedRouter(t)
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
	r, _ := newApplyTrackedRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/s/nonexistent-token", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown token, got %d", w.Code)
	}
}

func TestPublicSubscriptionRevokedToken404(t *testing.T) {
	r, _ := newApplyTrackedRouter(t)
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
	r, _ := newApplyTrackedRouter(t)
	plaintext, clientID := seedClientWithToken(t, r)
	// Disable the client.
	v1Request(t, r, http.MethodPut, "/api/v1/clients/"+clientID, `{"version":1,"name":"alice","enabled":false}`)
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
	r, _ := newApplyTrackedRouter(t)
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
	r, _ := newApplyTrackedRouter(t)
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
