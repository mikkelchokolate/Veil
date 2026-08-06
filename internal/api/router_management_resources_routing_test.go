package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagementAPIRoutingRulesRejectOversizedJSONBodies(t *testing.T) {
	r, _ := newTestRouter(ServerInfo{Version: "test", Mode: "dev"})
	oversizedMatch := "geosite:" + strings.Repeat("a", 1024*1024+1)

	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{
			name:   "create",
			method: http.MethodPost,
			path:   "/api/routing/rules",
			body:   `{"name":"huge-rule","match":"` + oversizedMatch + `","outbound":"direct","enabled":true}`,
		},
		{
			name:   "update",
			method: http.MethodPut,
			path:   "/api/routing/rules/default-direct",
			body:   `{"match":"` + oversizedMatch + `","outbound":"direct","enabled":true}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			w := httptest.NewRecorder()

			r.ServeHTTP(w, req)

			if w.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("expected 413 for oversized routing rule body, got %d with response length %d", w.Code, w.Body.Len())
			}
		})
	}
}

func TestManagementAPIUpdatesAndDeletesRoutingRuleByName(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	r, _ := newTestRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})

	create := httptest.NewRecorder()
	r.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/api/routing/rules", strings.NewReader(`{"name":"non-ru","match":"geosite:geolocation-!ru","outbound":"warp","enabled":false}`)))
	if create.Code != http.StatusCreated {
		t.Fatalf("create routing rule expected 201, got %d: %s", create.Code, create.Body.String())
	}

	update := httptest.NewRecorder()
	r.ServeHTTP(update, httptest.NewRequest(http.MethodPut, "/api/routing/rules/non-ru", strings.NewReader(`{"match":"geosite:openai","outbound":"direct","enabled":true}`)))
	if update.Code != http.StatusOK {
		t.Fatalf("update routing rule expected 200, got %d: %s", update.Code, update.Body.String())
	}
	var updated RoutingRule
	if err := json.NewDecoder(update.Body).Decode(&updated); err != nil {
		t.Fatalf("decode updated rule: %v", err)
	}
	if updated.Name != "non-ru" || updated.Match != "geosite:openai" || updated.Outbound != "direct" || !updated.Enabled {
		t.Fatalf("unexpected updated rule: %+v", updated)
	}

	restarted, _ := newTestRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})
	readAfterUpdate := httptest.NewRecorder()
	restarted.ServeHTTP(readAfterUpdate, httptest.NewRequest(http.MethodGet, "/api/routing/rules", nil))
	if !strings.Contains(readAfterUpdate.Body.String(), "geosite:openai") {
		t.Fatalf("persisted routing rule update missing: %s", readAfterUpdate.Body.String())
	}

	deleteRecorder := httptest.NewRecorder()
	restarted.ServeHTTP(deleteRecorder, httptest.NewRequest(http.MethodDelete, "/api/routing/rules/non-ru", nil))
	// DELETE returns 200 with the apply outcome (revision+job), not 204.
	if deleteRecorder.Code != http.StatusOK {
		t.Fatalf("delete routing rule expected 200, got %d: %s", deleteRecorder.Code, deleteRecorder.Body.String())
	}

	readAfterDelete := httptest.NewRecorder()
	restarted.ServeHTTP(readAfterDelete, httptest.NewRequest(http.MethodGet, "/api/routing/rules", nil))
	if strings.Contains(readAfterDelete.Body.String(), "non-ru") {
		t.Fatalf("deleted routing rule still present: %s", readAfterDelete.Body.String())
	}
}

func TestManagementAPIRejectsDuplicateRoutingRuleName(t *testing.T) {
	r, _ := newTestRouter(ServerInfo{Version: "test", Mode: "dev"})
	first := httptest.NewRecorder()
	r.ServeHTTP(first, httptest.NewRequest(http.MethodPost, "/api/routing/rules", strings.NewReader(`{"name":"ru-sites","match":"geosite:ru","outbound":"direct","enabled":true}`)))
	if first.Code != http.StatusCreated {
		t.Fatalf("create routing rule expected 201, got %d: %s", first.Code, first.Body.String())
	}

	duplicate := httptest.NewRecorder()
	r.ServeHTTP(duplicate, httptest.NewRequest(http.MethodPost, "/api/routing/rules", strings.NewReader(`{"name":"ru-sites","match":"geoip:ru","outbound":"direct","enabled":true}`)))
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate routing rule expected 409, got %d: %s", duplicate.Code, duplicate.Body.String())
	}
}

func TestManagementAPIGetsRoutingRuleByName(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	r, _ := newTestRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})

	create := httptest.NewRecorder()
	r.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/api/routing/rules", strings.NewReader(`{"name":"ru-sites","match":"geosite:ru","outbound":"direct","enabled":true}`)))
	if create.Code != http.StatusCreated {
		t.Fatalf("create routing rule expected 201, got %d: %s", create.Code, create.Body.String())
	}

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/routing/rules/ru-sites", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for GET named routing rule, got %d: %s", w.Code, w.Body.String())
	}
	var response RoutingRule
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Name != "ru-sites" || response.Match != "geosite:ru" || response.Outbound != "direct" || !response.Enabled {
		t.Fatalf("unexpected routing rule: %+v", response)
	}
}

func TestManagementAPIGetRoutingRuleByNameReturnsNotFoundForMissing(t *testing.T) {
	r, _ := newTestRouter(ServerInfo{Version: "test", Mode: "dev"})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/routing/rules/nonexistent", nil))

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing routing rule, got %d: %s", w.Code, w.Body.String())
	}
}

func TestManagementAPIExposesRoutingPresetProfiles(t *testing.T) {
	r, _ := newTestRouter(ServerInfo{Version: "test", Mode: "dev"})
	w := httptest.NewRecorder()

	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/routing/presets", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("routing presets expected 200, got %d: %s", w.Code, w.Body.String())
	}
	for _, want := range []string{"all", "all-except-Russia", "RU-blocked", "runetfreedom/russia-v2ray-rules-dat", "geoip.dat", "geoip.dat.sha256sum", "geosite.dat", "geosite.dat.sha256sum"} {
		if !strings.Contains(w.Body.String(), want) {
			t.Fatalf("routing presets response missing %q: %s", want, w.Body.String())
		}
	}
}

func TestManagementAPIAppliesRoutingPresetAndPersistsRules(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	r, _ := newTestRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})
	w := httptest.NewRecorder()

	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/routing/presets/all-except-Russia", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("apply preset expected 200, got %d: %s", w.Code, w.Body.String())
	}
	for _, want := range []string{"all-except-Russia", "geoip:ru", "geosite:category-ru", `"match":"all"`, `"outbound":"proxy"`} {
		if !strings.Contains(w.Body.String(), want) {
			t.Fatalf("preset response missing %q: %s", want, w.Body.String())
		}
	}

	restarted, _ := newTestRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})
	read := httptest.NewRecorder()
	restarted.ServeHTTP(read, httptest.NewRequest(http.MethodGet, "/api/routing/rules", nil))
	if !strings.Contains(read.Body.String(), "preset-all-except-russia") || !strings.Contains(read.Body.String(), "geosite:category-ru") {
		t.Fatalf("persisted preset routing rules missing: %s", read.Body.String())
	}
}

func TestManagementAPIRollsBackRoutingPresetOnSaveFailure(t *testing.T) {
	dir := t.TempDir()
	// Valid state path for startup; we break saves afterwards by replacing the
	// state file's parent with a non-directory.
	statePath := filepath.Join(dir, "state", "state.json")
	r, _ := newTestRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})

	// Baseline: no preset active.
	get := httptest.NewRecorder()
	r.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/routing/presets", nil))
	before := get.Body.String()

	// Break persistence: remove the state dir and put a file in its place.
	stateDir := filepath.Dir(statePath)
	if err := os.RemoveAll(stateDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stateDir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/routing/presets/all-except-Russia", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on save failure, got %d: %s", w.Code, w.Body.String())
	}

	// In-memory state must be rolled back — not left holding the preset rules.
	get2 := httptest.NewRecorder()
	r.ServeHTTP(get2, httptest.NewRequest(http.MethodGet, "/api/routing/presets", nil))
	if get2.Body.String() != before {
		t.Fatalf("preset state not rolled back after save failure:\nbefore: %s\nafter:  %s", before, get2.Body.String())
	}
}

func TestManagementAPIRejectsUnknownRoutingPreset(t *testing.T) {
	r, _ := newTestRouter(ServerInfo{Version: "test", Mode: "dev"})
	w := httptest.NewRecorder()

	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/routing/presets/not-real", nil))

	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown routing preset expected 404, got %d: %s", w.Code, w.Body.String())
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("expected no-store cache-control on API 404, got %q", cc)
	}
	if nosniff := w.Header().Get("X-Content-Type-Options"); nosniff != "nosniff" {
		t.Fatalf("expected nosniff on API 404, got %q", nosniff)
	}
}
