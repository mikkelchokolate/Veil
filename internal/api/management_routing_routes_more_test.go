package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
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

// #1410: create and update must reject an outbound the apply planner can
// never accept with 400 and leave desired state untouched, instead of
// returning 201/200 and blocking every later apply.
func TestHandleRoutingRulesRejectsUnknownOutbound(t *testing.T) {
	origAutoApply := autoApplyAfterMutation
	autoApplyAfterMutation = false
	t.Cleanup(func() { autoApplyAfterMutation = origAutoApply })
	state := newManagementState(ServerInfo{Mode: "dev"})
	admin := func(req *http.Request) *http.Request {
		req.Header.Set("Content-Type", "application/json")
		return req.WithContext(context.WithValue(req.Context(), contextKeyRole, "admin"))
	}
	before := len(state.rules)

	for _, outbound := range []string{"Direct", " warp", "block", "evil"} {
		body := `{"name":"r1","match":"geoip:private","outbound":` + strconv.Quote(outbound) + `,"enabled":true}`
		rec := httptest.NewRecorder()
		state.handleRoutingRules(rec, admin(httptest.NewRequest(http.MethodPost, "/api/routing/rules", strings.NewReader(body))))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("POST outbound=%q status=%d body=%s", outbound, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "outbound must be one of direct, proxy, warp") {
			t.Fatalf("POST outbound=%q body=%s", outbound, rec.Body.String())
		}
		if len(state.rules) != before {
			t.Fatalf("POST outbound=%q persisted a rule: %+v", outbound, state.rules)
		}
	}

	rec := httptest.NewRecorder()
	state.handleRoutingRules(rec, admin(httptest.NewRequest(http.MethodPost, "/api/routing/rules", strings.NewReader(`{"name":"r1","match":"geoip:private","outbound":"direct","enabled":true}`))))
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST valid status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	state.handleRoutingRuleByName(rec, admin(httptest.NewRequest(http.MethodPut, "/api/routing/rules/r1", strings.NewReader(`{"match":"geoip:private","outbound":"evilx","enabled":true}`))))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PUT status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(state.rules) != before+1 || state.rules[len(state.rules)-1].Outbound != "direct" {
		t.Fatalf("PUT with invalid outbound changed the rule: %+v", state.rules)
	}
}
