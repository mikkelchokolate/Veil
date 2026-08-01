package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestTokenMutationCannotCrossClientBoundary(t *testing.T) {
	r, _ := newApplyTrackedRouter(t)
	_, firstClient := seedClientWithToken(t, r)
	second := v1Request(t, r, http.MethodPost, "/api/v1/clients", `{"name":"bob","bindings":[{"inboundId":"hy2-sub","credential":"pw-bob"}]}`)
	if second.Code != http.StatusCreated {
		t.Fatalf("seed second client: %d %s", second.Code, second.Body.String())
	}
	var secondBody map[string]any
	_ = json.NewDecoder(second.Body).Decode(&secondBody)
	if nested, ok := secondBody["client"].(map[string]any); ok {
		secondBody = nested
	}
	secondClient := secondBody["id"].(string)
	list := v1Request(t, r, http.MethodGet, "/api/v1/clients/"+firstClient+"/tokens", "")
	var payload struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	_ = json.NewDecoder(list.Body).Decode(&payload)
	if len(payload.Items) != 1 {
		t.Fatalf("token list: %s", list.Body.String())
	}
	for _, target := range []struct{ method, suffix string }{{http.MethodDelete, ""}, {http.MethodPost, "/rotate"}} {
		response := v1Request(t, r, target.method, "/api/v1/clients/"+secondClient+"/tokens/"+payload.Items[0].ID+target.suffix, "")
		if response.Code != http.StatusNotFound {
			t.Fatalf("cross-client %s returned %d: %s", target.method, response.Code, response.Body.String())
		}
	}
}

func TestExpiredTokenRotationRequiresFutureExpiry(t *testing.T) {
	r, state := newApplyTrackedRouterWithState(t)
	_, clientID := seedClientWithToken(t, r)
	initialExpiry := time.Now().Add(time.Hour).Unix()
	issued := v1Request(t, r, http.MethodPost, "/api/v1/clients/"+clientID+"/tokens", fmt.Sprintf(`{"label":"expired","expiresAt":%d}`, initialExpiry))
	if issued.Code != http.StatusCreated {
		t.Fatalf("issue expired token: %d %s", issued.Code, issued.Body.String())
	}
	var issuedBody struct {
		Token struct {
			ID string `json:"id"`
		} `json:"token"`
	}
	_ = json.NewDecoder(issued.Body).Decode(&issuedBody)
	expired := time.Now().Add(-time.Hour).Unix()
	if _, err := state.db.Exec(`UPDATE subscription_tokens SET expires_at=? WHERE id=?`, expired, issuedBody.Token.ID); err != nil {
		t.Fatalf("expire token: %v", err)
	}
	rotationURL := "/api/v1/clients/" + clientID + "/tokens/" + issuedBody.Token.ID + "/rotate"
	if response := v1Request(t, r, http.MethodPost, rotationURL, ""); response.Code != http.StatusBadRequest {
		t.Fatalf("expired rotation without expiry: %d %s", response.Code, response.Body.String())
	}
	future := time.Now().Add(time.Hour).Unix()
	if response := v1Request(t, r, http.MethodPost, rotationURL, fmt.Sprintf(`{"expiresAt":%d}`, future)); response.Code != http.StatusOK {
		t.Fatalf("expired rotation with future expiry: %d %s", response.Code, response.Body.String())
	}
}

func TestClientBindingRequiresExistingInboundAndInboundDeleteRejectsReference(t *testing.T) {
	r, _ := newApplyTrackedRouter(t)
	missing := v1Request(t, r, http.MethodPost, "/api/v1/clients", `{"name":"bad","bindings":[{"inboundId":"missing","credential":"pw"}]}`)
	if missing.Code != http.StatusBadRequest {
		t.Fatalf("missing inbound binding: %d %s", missing.Code, missing.Body.String())
	}
	_, _ = seedClientWithToken(t, r)
	deleted := v1Request(t, r, http.MethodDelete, "/api/inbounds/hy2-sub", "")
	if deleted.Code != http.StatusConflict {
		t.Fatalf("referenced inbound delete: %d %s", deleted.Code, deleted.Body.String())
	}
}
