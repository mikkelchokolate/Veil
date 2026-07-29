package api

import (
	"net/http"
	"testing"
)

func TestTokenIssueVerifiesParentInSameTransaction(t *testing.T) {
	router, state := newApplyTrackedRouterWithState(t)
	response := v1Request(t, router, http.MethodPost, "/api/v1/clients/missing-parent/tokens", `{"label":"orphan-attempt"}`)
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing-parent token issue status=%d body=%s", response.Code, response.Body.String())
	}
	var count int
	if err := state.db.QueryRow(`SELECT COUNT(*) FROM subscription_tokens WHERE client_id='missing-parent'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("orphan subscription token rows=%d", count)
	}
}
