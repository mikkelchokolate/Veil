package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// #1272 items 2/4 (#1397 review): the public rotate route must return a
// replacement that keeps the old token's issuer and enabled flag, as the
// rotate body description in docs/openapi.yaml promises.
func TestV1RotateKeepsIssuerAndDisabledFlag(t *testing.T) {
	router, state := newSubscriptionTestRouter(t)
	_, clientID := seedClientWithToken(t, router)

	issued := v1Request(t, router, http.MethodPost, "/api/v1/clients/"+clientID+"/tokens", `{"label":"paused"}`)
	if issued.Code != http.StatusCreated {
		t.Fatalf("issue: %d %s", issued.Code, issued.Body.String())
	}
	type tokenView struct {
		ID        string `json:"id"`
		Label     string `json:"label"`
		CreatedBy string `json:"createdBy"`
		Enabled   bool   `json:"enabled"`
	}
	var created struct {
		Token tokenView `json:"token"`
	}
	if err := json.Unmarshal(issued.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Token.CreatedBy == "" {
		t.Fatalf("issue did not record the actor: %s", issued.Body.String())
	}
	// No route disables a token today; flip the flag at the store level.
	if err := state.tokenStore.SetEnabled(created.Token.ID, false); err != nil {
		t.Fatalf("disable: %v", err)
	}

	rotated := v1Request(t, router, http.MethodPost, "/api/v1/clients/"+clientID+"/tokens/"+created.Token.ID+"/rotate", `{}`)
	if rotated.Code != http.StatusOK {
		t.Fatalf("rotate: %d %s", rotated.Code, rotated.Body.String())
	}
	var got struct {
		Token tokenView `json:"token"`
	}
	if err := json.Unmarshal(rotated.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Token.ID == created.Token.ID {
		t.Fatalf("rotate returned the old token id")
	}
	if got.Token.Enabled {
		t.Fatalf("rotate re-enabled a disabled token: %s", rotated.Body.String())
	}
	if got.Token.CreatedBy != created.Token.CreatedBy || got.Token.Label != "paused" {
		t.Fatalf("rotate dropped issuer/label: got createdBy %q label %q, want %q/paused",
			got.Token.CreatedBy, got.Token.Label, created.Token.CreatedBy)
	}
}
