package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestAuthMiddlewareBlocksAPIWhenStartupStateUnavailable(t *testing.T) {
	state := &managementState{startupStateLoadFailed: true}
	handler := authMiddlewareWithOptions(state, authMiddlewareOptions{
		Token:             "static-token",
		AllowDevAnonymous: true,
		AllowSetup:        true,
	}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("API handler ran as %v", r.Context().Value(contextKeyUsername))
	}))

	for _, path := range []string{"/api/auth/login", "/api/setup/status", "/api/settings"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-Veil-Token", "static-token")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("path=%s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestStartupStateLoadFailureDisablesManagementAPI(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	if err := os.WriteFile(statePath, []byte("not encrypted state"), 0o600); err != nil {
		t.Fatal(err)
	}

	state := newManagementState(ServerInfo{
		StatePath: statePath,
		KeyPath:   filepath.Join(root, "state.key"),
		ApplyRoot: filepath.Join(root, "apply"),
	})
	if !state.startupStateLoadFailed {
		t.Fatal("startup state load failure was not recorded")
	}
	if state.allowDevAnonymous {
		t.Fatal("startup state load failure left anonymous admin enabled")
	}
}

func TestStartupKeyFailureSkipsStateLoadAndDisablesManagementAPI(t *testing.T) {
	root := t.TempDir()
	blockedParent := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(blockedParent, []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}

	state := newManagementState(ServerInfo{
		StatePath: filepath.Join(root, "missing-state.json"),
		KeyPath:   filepath.Join(blockedParent, "state.key"),
		ApplyRoot: filepath.Join(root, "apply"),
	})
	if !state.startupStateLoadFailed {
		t.Fatal("startup key failure was not recorded")
	}
	if state.cipher != nil {
		t.Fatal("startup key failure unexpectedly produced a cipher")
	}
	if state.allowDevAnonymous {
		t.Fatal("startup key failure left anonymous admin enabled")
	}
}

func TestMissingStartupStateKeepsFirstRunAnonymousMode(t *testing.T) {
	root := t.TempDir()
	state := newManagementState(ServerInfo{
		StatePath: filepath.Join(root, "state.json"),
		KeyPath:   filepath.Join(root, "state.key"),
		ApplyRoot: filepath.Join(root, "apply"),
	})
	if state.startupStateLoadFailed {
		t.Fatal("missing first-run state was treated as a load failure")
	}
	if !state.allowDevAnonymous {
		t.Fatal("first-run anonymous mode was unexpectedly disabled")
	}
	if state.cipher == nil {
		t.Fatal("first-run state did not initialize an encryption cipher")
	}
}
