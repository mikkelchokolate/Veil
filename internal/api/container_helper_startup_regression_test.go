package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/privileged"
	"github.com/mikkelchokolate/Veil/internal/statecommit"
)

func TestMissingHelperDoesNotFailClosedWithoutRotationJournal(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	router, reloader := newTestRouter(ServerInfo{
		Version:                 "test",
		Mode:                    "server",
		StatePath:               statePath,
		KeyPath:                 filepath.Join(root, "state.key"),
		ApplyRoot:               filepath.Join(root, "apply"),
		RequirePrivilegedHelper: true,
		Privileged:              privileged.NewSocketClient(filepath.Join(root, "helper.sock")),
		SetupAllowed:            true,
	})
	if state, ok := reloader.(*managementState); ok {
		t.Cleanup(func() { _ = state.Close() })
		if state.startupStateLoadFailed {
			t.Fatalf("missing helper without a rotation journal fail-closed startup: %v", state.startupStateLoadErr)
		}
	}

	for _, path := range []string{"/api/auth/status", "/api/setup/status"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code == http.StatusServiceUnavailable {
			t.Fatalf("%s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestMissingHelperFailClosesWhenRotationJournalExists(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	if err := os.WriteFile(statecommit.KeyRotationJournalPath(statePath), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	router, reloader := newTestRouter(ServerInfo{
		Version:                 "test",
		Mode:                    "server",
		StatePath:               statePath,
		KeyPath:                 filepath.Join(root, "state.key"),
		ApplyRoot:               filepath.Join(root, "apply"),
		RequirePrivilegedHelper: true,
		Privileged:              privileged.NewSocketClient(filepath.Join(root, "helper.sock")),
		SetupAllowed:            true,
	})
	if state, ok := reloader.(*managementState); ok {
		t.Cleanup(func() { _ = state.Close() })
		if !state.startupStateLoadFailed {
			t.Fatal("pending rotation journal with a missing helper did not fail-close startup")
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("auth status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHealthzUnhealthyWhenStartupStateLoadFailed(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	if err := os.WriteFile(statePath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	router, reloader := newTestRouter(ServerInfo{
		Version:   "test",
		StatePath: statePath,
		KeyPath:   filepath.Join(root, "state.key"),
		ApplyRoot: filepath.Join(root, "apply"),
	})
	state, ok := reloader.(*managementState)
	if !ok {
		t.Fatalf("reloader is not *managementState: %T", reloader)
	}
	t.Cleanup(func() { _ = state.Close() })
	state.mu.Lock()
	state.startupStateLoadFailed = true
	state.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("healthz status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "unhealthy" {
		t.Fatalf("healthz body=%v", body)
	}
}
