package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRURecommendedPreviewRejectsOversizedJSONBody(t *testing.T) {
	r, _ := newTestRouter(ServerInfo{Version: "test", Mode: "dev"})
	body := strings.NewReader(`{"domain":"` + strings.Repeat("a", 1024*1024+1) + `","email":"admin@example.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/profiles/ru-recommended/preview", body)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for oversized preview body, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRURecommendedPreviewResponseOmitsRemovedStackFields(t *testing.T) {
	r, _ := newTestRouter(ServerInfo{Version: "test", Mode: "dev"})
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
	r, _ := newTestRouter(ServerInfo{Version: "test", Mode: "dev"})
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
	r, _ := newTestRouter(ServerInfo{Version: "test", Mode: "dev"})
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
	if !strings.Contains(response.Caddyfile, "example.com") || !strings.Contains(response.Caddyfile, "127.0.0.1:2096") {
		t.Fatalf("unexpected Panel Caddy JSON:\n%s", response.Caddyfile)
	}
}

func TestRURecommendedPreviewCaddyAccessMatchesOpenAPICaddyfileField(t *testing.T) {
	r, _ := newTestRouter(ServerInfo{Version: "test", Mode: "dev"})
	req := httptest.NewRequest(http.MethodPost, "/api/profiles/ru-recommended/preview", strings.NewReader(
		`{"domain":"example.com","email":"admin@example.com","panelAccess":"caddy"}`,
	))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var contract struct {
		Domain      string `json:"domain"`
		Email       string `json:"email"`
		PanelAccess string `json:"panelAccess"`
		PanelURL    string `json:"panelUrl"`
		Caddyfile   string `json:"caddyfile"`
		CaddyJSON   string `json:"caddyJSON"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &contract); err != nil {
		t.Fatalf("decode OpenAPI field names: %v", err)
	}
	if contract.PanelAccess != "caddy" || contract.PanelURL == "" {
		t.Fatalf("expected populated panelUrl for caddy access: %+v", contract)
	}
	if contract.Caddyfile == "" || !strings.Contains(contract.Caddyfile, "example.com") || !strings.Contains(contract.Caddyfile, "127.0.0.1:2096") {
		t.Fatalf("OpenAPI caddyfile field missing Caddy config: %+v", contract)
	}
	if contract.CaddyJSON != "" {
		t.Fatalf("handler must not emit caddyJSON: %s", w.Body.String())
	}
}

func TestRURecommendedPreviewEndpointRejectsRemovedStackField(t *testing.T) {
	r, _ := newTestRouter(ServerInfo{Version: "test", Mode: "dev"})
	body := strings.NewReader(`{"domain":"example.com","email":"admin@example.com","stack":"hysteria2"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/profiles/ru-recommended/preview", body)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest || !strings.Contains(responseErrorMessage(t, w.Body.Bytes()), `json: unknown field "stack"`) {
		t.Fatalf("expected removed stack field rejection, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRURecommendedPreviewEndpointRequiresDomainEmailForPanelCaddy(t *testing.T) {
	r, _ := newTestRouter(ServerInfo{Version: "test", Mode: "dev"})
	body := strings.NewReader(`{"panelAccess":"caddy"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/profiles/ru-recommended/preview", body)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}
