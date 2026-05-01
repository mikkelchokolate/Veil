package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRouterHealthz(t *testing.T) {
	r := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "ok\n" {
		t.Fatalf("unexpected body: %q", w.Body.String())
	}
}

func TestRouterServesPanelShell(t *testing.T) {
	r := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Fatalf("unexpected content-type: %q", ct)
	}
	if body := w.Body.String(); !strings.Contains(body, "Veil Panel") || !strings.Contains(body, "/api/status") {
		t.Fatalf("unexpected panel body: %s", body)
	}
}

func TestRURecommendedPreviewEndpoint(t *testing.T) {
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	body := strings.NewReader(`{"domain":"example.com","email":"admin@example.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/profiles/ru-recommended/preview", body)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var response map[string]any
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response["domain"] != "example.com" {
		t.Fatalf("unexpected response: %+v", response)
	}
	if response["caddyfile"] == "" || response["hysteria2YAML"] == "" {
		t.Fatalf("expected rendered configs: %+v", response)
	}
}

func TestRURecommendedPreviewEndpointHonorsStack(t *testing.T) {
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	body := strings.NewReader(`{"domain":"example.com","email":"admin@example.com","stack":"naive"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/profiles/ru-recommended/preview", body)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var response map[string]any
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response["stack"] != "naive" {
		t.Fatalf("expected stack naive, got %+v", response)
	}
	if response["caddyfile"] == "" || response["hysteria2YAML"] != "" {
		t.Fatalf("expected only naive preview output: %+v", response)
	}
	if response["naiveClientURL"] == "" || response["hysteria2ClientURI"] != "" {
		t.Fatalf("expected only naive client link: %+v", response)
	}
}

func TestRURecommendedPreviewEndpointRejectsInvalidStack(t *testing.T) {
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	body := strings.NewReader(`{"domain":"example.com","email":"admin@example.com","stack":"bad"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/profiles/ru-recommended/preview", body)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSpeedtestEndpointRunsConfiguredRunner(t *testing.T) {
	old := speedtestRunner
	speedtestRunner = func(r *http.Request) (SpeedtestResult, error) {
		return SpeedtestResult{
			Server:       "Test ISP - Moscow",
			PingMS:       12.3,
			DownloadMbps: 101.5,
			UploadMbps:   42.7,
		}, nil
	}
	defer func() { speedtestRunner = old }()

	r := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	req := httptest.NewRequest(http.MethodPost, "/api/tools/speedtest", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var response SpeedtestResult
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.DownloadMbps != 101.5 || response.UploadMbps != 42.7 || response.PingMS != 12.3 {
		t.Fatalf("unexpected speedtest result: %+v", response)
	}
	if response.Server != "Test ISP - Moscow" {
		t.Fatalf("unexpected server: %+v", response)
	}
}

func TestSpeedtestEndpointReportsRunnerError(t *testing.T) {
	old := speedtestRunner
	speedtestRunner = func(r *http.Request) (SpeedtestResult, error) {
		return SpeedtestResult{}, errSpeedtestUnavailable
	}
	defer func() { speedtestRunner = old }()

	r := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	req := httptest.NewRequest(http.MethodPost, "/api/tools/speedtest", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRouterServesPanelShellWithSpeedtestControl(t *testing.T) {
	r := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "Speedtest") || !strings.Contains(body, "/api/tools/speedtest") {
		t.Fatalf("expected speedtest control in panel shell: %s", body)
	}
}

func TestRouterStatus(t *testing.T) {
	r := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var body StatusResponse
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Name != "Veil" || body.Version != "test" || body.Mode != "dev" {
		t.Fatalf("unexpected status: %+v", body)
	}
	if len(body.Services) != 3 {
		t.Fatalf("expected 3 services, got %+v", body.Services)
	}
}
