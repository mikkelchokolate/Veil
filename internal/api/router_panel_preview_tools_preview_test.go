package api

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var _router_panel_preview_tools_preview_deps = []any{
	bytes.Buffer{}, sha256.Sum256, tls.VersionTLS12, base64.StdEncoding, hex.EncodeToString, json.NewDecoder, errors.New, fmt.Sprintf, log.Printf, http.MethodGet, httptest.NewRecorder, os.ReadFile, filepath.Join, strings.Contains, testing.T{},
}

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
	if response.Stack != "panel" || response.InstallNaive || response.InstallHysteria2 || response.InstallMieru || response.Caddyfile != "" || response.Hysteria2YAML != "" {
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
	if response.Stack != "panel" || response.PanelAccess != "caddy" || response.PanelURL == "" || response.Caddyfile == "" {
		t.Fatalf("expected Panel Caddy preview: %+v", response)
	}
	if response.InstallNaive || response.InstallHysteria2 || response.InstallMieru || response.Hysteria2YAML != "" || response.NaiveClientURL != "" || response.Hysteria2ClientURI != "" {
		t.Fatalf("Panel Caddy preview should not render protocol artifacts: %+v", response)
	}
	if strings.Contains(response.Caddyfile, "forward_proxy") || !strings.Contains(response.Caddyfile, "reverse_proxy 127.0.0.1:") {
		t.Fatalf("unexpected Panel Caddyfile:\n%s", response.Caddyfile)
	}
}

func TestRURecommendedPreviewEndpointRejectsProtocolStack(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	body := strings.NewReader(`{"domain":"example.com","email":"admin@example.com","stack":"naive"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/profiles/ru-recommended/preview", body)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "only supports Panel") {
		t.Fatalf("expected protocol stack rejection, got %d: %s", w.Code, w.Body.String())
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
