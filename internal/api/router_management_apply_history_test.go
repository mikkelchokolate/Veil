package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/atomicfile"
)

func TestManagementApplyHistoryRetentionKeepsNewestEntries(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := writeRenderableManagementState(statePath, "naive-only"); err != nil {
		t.Fatalf("write state: %v", err)
	}
	applyRoot := t.TempDir()
	seed := make([]ApplyHistoryEntry, 100)
	for i := range seed {
		seed[i] = ApplyHistoryEntry{ID: fmt.Sprintf("old-%03d", i), Timestamp: fmt.Sprintf("2026-05-01T00:00:%02dZ", i%60), Stage: "staged", Success: true}
	}
	body, err := json.MarshalIndent(seed, "", "  ")
	if err != nil {
		t.Fatalf("marshal seed history: %v", err)
	}
	if err := atomicfile.Write(filepath.Join(applyRoot, "generated", "veil", "apply-history.json"), append(body, '\n'), 0o600, 0o700); err != nil {
		t.Fatalf("write seed history: %v", err)
	}
	oldValidator := stagedConfigValidator
	defer func() { stagedConfigValidator = oldValidator }()
	stagedConfigValidator = func(paths []string) []ConfigValidationResult {
		return []ConfigValidationResult{{Name: "caddy", Config: paths[0], Valid: true}}
	}
	r, _ := newTestRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: applyRoot})
	w := httptest.NewRecorder()

	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/apply", strings.NewReader(`{"confirm":true}`)))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var history []ApplyHistoryEntry
	body, err = os.ReadFile(filepath.Join(applyRoot, "generated", "veil", "apply-history.json"))
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	if err := json.Unmarshal(body, &history); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if len(history) != 100 {
		t.Fatalf("expected capped history length 100, got %d", len(history))
	}
	if history[0].ID == "" || !history[0].Success || history[0].Stage != "staged" {
		t.Fatalf("expected newest apply entry first, got %+v", history[0])
	}
	if history[len(history)-1].ID != "old-098" {
		t.Fatalf("expected oldest retained entry to be old-098 after trimming old-099, got %+v", history[len(history)-1])
	}
}

func TestManagementApplyHistoryEndpointReturnsNewestFirstAndPersistsAcrossRouters(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := writeRenderableManagementState(statePath, "naive-only"); err != nil {
		t.Fatalf("write state: %v", err)
	}
	applyRoot := t.TempDir()
	oldValidator := stagedConfigValidator
	defer func() { stagedConfigValidator = oldValidator }()
	stagedConfigValidator = func(paths []string) []ConfigValidationResult {
		return []ConfigValidationResult{{Name: "caddy", Config: paths[0], Valid: true}}
	}
	r, _ := newTestRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: applyRoot})
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/apply", strings.NewReader(`{"confirm":true}`)))
		if w.Code != http.StatusOK {
			t.Fatalf("apply %d expected 200, got %d: %s", i, w.Code, w.Body.String())
		}
	}
	freshRouter, _ := newTestRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: applyRoot})
	w := httptest.NewRecorder()
	freshRouter.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/apply/history", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var history []ApplyHistoryEntry
	if err := json.NewDecoder(w.Body).Decode(&history); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("expected two history entries, got %+v", history)
	}
	if history[0].ID == history[1].ID || history[0].Timestamp < history[1].Timestamp {
		t.Fatalf("expected unique newest-first entries: %+v", history)
	}
	if history[0].Stage != "staged" || !history[0].Success || history[0].LiveApplied || history[0].ServicesApplied {
		t.Fatalf("unexpected staged history entry: %+v", history[0])
	}
}

func TestManagementApplyHistoryEndpointFiltersStageSuccessAndLimit(t *testing.T) {
	applyRoot := t.TempDir()
	history := []ApplyHistoryEntry{
		{ID: "4", Timestamp: "2026-05-01T00:00:04Z", Stage: "rollback", Success: false},
		{ID: "3", Timestamp: "2026-05-01T00:00:03Z", Stage: "rollback", Success: false},
		{ID: "2", Timestamp: "2026-05-01T00:00:02Z", Stage: "live", Success: true},
		{ID: "1", Timestamp: "2026-05-01T00:00:01Z", Stage: "staged", Success: true},
	}
	body, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		t.Fatalf("marshal history: %v", err)
	}
	if err := atomicfile.Write(filepath.Join(applyRoot, "generated", "veil", "apply-history.json"), append(body, '\n'), 0o600, 0o700); err != nil {
		t.Fatalf("write history: %v", err)
	}
	r, _ := newTestRouter(ServerInfo{Version: "test", Mode: "dev", ApplyRoot: applyRoot})
	w := httptest.NewRecorder()

	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/apply/history?stage=rollback&success=false&limit=1", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var filtered []ApplyHistoryEntry
	if err := json.NewDecoder(w.Body).Decode(&filtered); err != nil {
		t.Fatalf("decode filtered history: %v", err)
	}
	if len(filtered) != 1 || filtered[0].ID != "4" || filtered[0].Stage != "rollback" || filtered[0].Success {
		t.Fatalf("unexpected filtered history: %+v", filtered)
	}
}

func TestManagementApplyHistoryEndpointRejectsInvalidFilterNamesAndValues(t *testing.T) {
	r, _ := newTestRouter(ServerInfo{Version: "test", Mode: "dev", ApplyRoot: t.TempDir()})
	cases := []string{
		"/api/apply/history?success=maybe",
		"/api/apply/history?limit=-1",
		"/api/apply/history?limit=abc",
		"/api/apply/history?stage=unknown",
		"/api/apply/history?offset=1",
	}
	for _, path := range cases {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s expected 400, got %d: %s", path, w.Code, w.Body.String())
		}
	}
}

func TestManagementApplyHistoryEndpointReportsFirstUnsupportedFilterDeterministically(t *testing.T) {
	r, _ := newTestRouter(ServerInfo{Version: "test", Mode: "dev", ApplyRoot: t.TempDir()})

	for i := 0; i < 50; i++ {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/apply/history?zzz=1&aaa=1", nil))

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for unsupported filters, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "invalid history filter: aaa") {
			t.Fatalf("expected deterministic first unsupported filter aaa, got %q", w.Body.String())
		}
	}
}
