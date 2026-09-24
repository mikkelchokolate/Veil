package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/routing"
)

func TestWriteRoutingRuleManagementError(t *testing.T) {
	cases := []struct {
		err        error
		wantStatus int
		wantBody   string
	}{
		{routing.ErrRoutingRuleInvalid, http.StatusBadRequest, "name, match, and outbound"},
		{routing.ErrRoutingRuleDuplicateName, http.StatusConflict, "routing rule name already exists"},
		{routing.ErrRoutingRuleNotFound, http.StatusNotFound, "404"},
		{errors.New("custom failure"), http.StatusInternalServerError, "internal server error"},
	}
	for _, tc := range cases {
		t.Run(tc.err.Error(), func(t *testing.T) {
			rec := httptest.NewRecorder()
			writeRoutingRuleManagementError(rec, tc.err)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status=%d", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), tc.wantBody) {
				t.Fatalf("body=%s", rec.Body.String())
			}
		})
	}
}

func TestHandleRoutingPresets(t *testing.T) {
	state := newManagementState(ServerInfo{Mode: "dev"})
	req := httptest.NewRequest(http.MethodGet, "/api/routing/presets", nil)
	rec := httptest.NewRecorder()
	state.handleRoutingPresets(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if _, ok := response["presets"]; !ok {
		t.Fatalf("expected presets, got %+v", response)
	}
}

func TestHandleRoutingPresetByNameValidation(t *testing.T) {
	state := newManagementState(ServerInfo{Mode: "dev"})

	notFound := httptest.NewRequest(http.MethodPost, "/api/routing/presets/unknown", nil)
	rec := httptest.NewRecorder()
	state.handleRoutingPresetByName(rec, notFound)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown preset status=%d body=%s", rec.Code, rec.Body.String())
	}

	get := httptest.NewRequest(http.MethodGet, "/api/routing/presets/RU-blocked", nil)
	rec = httptest.NewRecorder()
	state.handleRoutingPresetByName(rec, get)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET status=%d", rec.Code)
	}
}

func TestHandleRoutingRules(t *testing.T) {
	// This is a handler-level test with no apply infrastructure; without a
	// StatePath the legacy auto-apply cannot converge and would report
	// success:false for unrelated environmental reasons. Disable auto-apply
	// so the success assertion checks the mutation contract deterministically.
	origAutoApply := autoApplyAfterMutation
	autoApplyAfterMutation = false
	t.Cleanup(func() { autoApplyAfterMutation = origAutoApply })
	state := newManagementState(ServerInfo{Mode: "dev"})

	get := httptest.NewRequest(http.MethodGet, "/api/routing/rules", nil)
	get = get.WithContext(context.WithValue(get.Context(), contextKeyRole, "admin"))
	rec := httptest.NewRecorder()
	state.handleRoutingRules(rec, get)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", rec.Code, rec.Body.String())
	}

	post := httptest.NewRequest(http.MethodPost, "/api/routing/rules", strings.NewReader(`{"name":"block","match":"geosite:example","outbound":"direct","enabled":true}`))
	post.Header.Set("Content-Type", "application/json")
	post = post.WithContext(context.WithValue(post.Context(), contextKeyRole, "admin"))
	rec = httptest.NewRecorder()
	state.handleRoutingRules(rec, post)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST status=%d body=%s", rec.Code, rec.Body.String())
	}
	// The mutation envelope must report the apply outcome honestly (#835).
	requireMutationEnvelopeSuccess(t, rec.Body.Bytes(), "create routing rule")
	// Creating an enabled geosite rule must materialize the default dat
	// source via ensureRoutingDatSourceLocked — otherwise the matcher is
	// silently dropped at render time.
	if len(state.routingSource.Files) != 2 ||
		state.routingSource.Files[0].Name != "geoip.dat" ||
		state.routingSource.Files[1].Name != "geosite.dat" {
		t.Fatalf("geosite rule did not materialize dat source files: %+v", state.routingSource.Files)
	}
}
