package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPanelRegisterRetainsSPAHandler(t *testing.T) {
	routes := PanelRoutes{Info: ServerInfo{Version: "test"}, BasePath: "/"}
	routes.Register(http.NewServeMux())
	if routes.spa == nil {
		t.Fatal("expected Register to retain the SPA handler")
	}
}

func TestPanelFavicon(t *testing.T) {
	routes := PanelRoutes{Info: ServerInfo{Version: "test"}}

	get := httptest.NewRequest(http.MethodGet, "/favicon.ico", nil)
	rec := httptest.NewRecorder()
	routes.handleFavicon(rec, get)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status=%d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/svg+xml" {
		t.Fatalf("Content-Type=%q", ct)
	}
	if !strings.Contains(rec.Body.String(), "<svg") {
		t.Fatal("expected svg body")
	}

	head := httptest.NewRequest(http.MethodHead, "/favicon.ico", nil)
	rec = httptest.NewRecorder()
	routes.handleFavicon(rec, head)
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Fatalf("HEAD status=%d body=%q", rec.Code, rec.Body.String())
	}

	post := httptest.NewRequest(http.MethodPost, "/favicon.ico", nil)
	rec = httptest.NewRecorder()
	routes.handleFavicon(rec, post)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status=%d", rec.Code)
	}
}

func TestPanelHealthReportsUnhealthyWhenStatePathMissing(t *testing.T) {
	routes := PanelRoutes{Info: ServerInfo{Version: "test", StatePath: "/nonexistent/path/state.json"}}
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	routes.handleHealth(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "unhealthy") {
		t.Fatalf("expected unhealthy body, got %s", rec.Body.String())
	}
}

func TestPanelHealthHeadHasNoBody(t *testing.T) {
	tmp := t.TempDir()
	statePath := filepath.Join(tmp, "state.json")
	if err := os.WriteFile(statePath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	routes := PanelRoutes{Info: ServerInfo{Version: "test", StatePath: statePath}}
	req := httptest.NewRequest(http.MethodHead, "/healthz", nil)
	rec := httptest.NewRecorder()
	routes.handleHealth(rec, req)
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Fatalf("HEAD status=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestPanelVersionHeadHasNoBody(t *testing.T) {
	routes := PanelRoutes{Info: ServerInfo{Version: "0.6.0"}}
	req := httptest.NewRequest(http.MethodHead, "/api/version", nil)
	rec := httptest.NewRecorder()
	routes.handleVersion(rec, req)
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Fatalf("HEAD status=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestPanelRootRejectsNonRootPaths(t *testing.T) {
	state := newManagementState(ServerInfo{Mode: "dev"})
	routes := PanelRoutes{Info: ServerInfo{Version: "test"}, State: state}
	req := httptest.NewRequest(http.MethodGet, "/not-root", nil)
	rec := httptest.NewRecorder()
	routes.handlePanel(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
