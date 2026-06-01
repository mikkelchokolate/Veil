package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRURecommendedPreviewRejectsOversizedJSONBody(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	body := strings.NewReader(`{"domain":"` + strings.Repeat("a", 1024*1024+1) + `","email":"admin@example.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/profiles/ru-recommended/preview", body)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for oversized preview body, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRURecommendedPreviewResponseOmitsRemovedStackFields(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	body := strings.NewReader(`{"domain":"example.com","email":"admin@example.com"}`)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/profiles/ru-recommended/preview", body))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	for _, unwanted := range []string{`"stack"`, `"installNaive"`, `"installHysteria2"`, `"installMieru"`, `"naiveClientURL"`, `"hysteria2ClientURI"`, `"hysteria2YAML"`} {
		if strings.Contains(w.Body.String(), unwanted) {
			t.Fatalf("profile preview response should not expose removed stack/protocol install field %s: %s", unwanted, w.Body.String())
		}
	}
}

func TestRURecommendedPreviewEndpointDefaultsToPanelOnly(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	body := strings.NewReader(`{"domain":"example.com","email":"admin@example.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/profiles/ru-recommended/preview", body)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var response RURecommendedPreviewResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Domain != "example.com" || response.Email != "admin@example.com" || response.Caddyfile != "" {
		t.Fatalf("preview should default to Panel-only: %+v", response)
	}
}

func TestRURecommendedPreviewEndpointRendersPanelCaddyAccess(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	body := strings.NewReader(`{"domain":"example.com","email":"admin@example.com","panelAccess":"caddy"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/profiles/ru-recommended/preview", body)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var response RURecommendedPreviewResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.PanelAccess != "caddy" || response.PanelURL == "" || response.Caddyfile == "" {
		t.Fatalf("expected Panel Caddy preview: %+v", response)
	}
	if strings.Contains(response.Caddyfile, "forward_proxy") || !strings.Contains(response.Caddyfile, "reverse_proxy 127.0.0.1:") {
		t.Fatalf("unexpected Panel Caddyfile:\n%s", response.Caddyfile)
	}
}

func TestRURecommendedPreviewEndpointRejectsRemovedStackField(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	body := strings.NewReader(`{"domain":"example.com","email":"admin@example.com","stack":"hysteria2"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/profiles/ru-recommended/preview", body)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), `json: unknown field "stack"`) {
		t.Fatalf("expected removed stack field rejection, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRURecommendedPreviewEndpointRequiresDomainEmailForPanelCaddy(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	body := strings.NewReader(`{"panelAccess":"caddy"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/profiles/ru-recommended/preview", body)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}
