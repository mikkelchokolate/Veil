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

var _routerTestDeps_router_client_links_test = []any{
	bytes.Buffer{}, sha256.Sum256, tls.VersionTLS12, base64.StdEncoding, hex.EncodeToString, json.NewDecoder, errors.New, fmt.Sprintf, log.Printf, http.MethodGet, httptest.NewRecorder, os.ReadFile, filepath.Join, strings.Contains, testing.T{},
}

func TestClientLinksEndpointBuildsEnabledProxyLinks(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := writeRenderableManagementState(statePath, "both"); err != nil {
		t.Fatalf("write state: %v", err)
	}
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})
	req := httptest.NewRequest(http.MethodGet, "/api/client-links", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var response ClientLinksResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode client links: %v", err)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("expected no-store cache-control for secret-bearing client links, got %q", cc)
	}
	if nosniff := w.Header().Get("X-Content-Type-Options"); nosniff != "nosniff" {
		t.Fatalf("expected nosniff for secret-bearing client links, got %q", nosniff)
	}
	if pragma := w.Header().Get("Pragma"); pragma != "no-cache" {
		t.Fatalf("expected no-cache Pragma for secret-bearing client links, got %q", pragma)
	}
	if response.Domain != "vpn.example.com" || response.Stack != "panel" || response.SubscriptionURL != "/api/client-links/subscription" || response.Base64SubscriptionURL != "/api/client-links/subscription?format=base64" || response.RawSubscriptionURL != "/api/client-links/subscription?format=raw" || response.Count != 2 {
		t.Fatalf("unexpected client link metadata: %+v", response)
	}
	if response.SchemaVersion != "v1" {
		t.Fatalf("unexpected client links schema version: %q", response.SchemaVersion)
	}
	if response.DefaultSubscriptionFormat != "base64" {
		t.Fatalf("unexpected default subscription format: %q", response.DefaultSubscriptionFormat)
	}
	if response.Base64SubscriptionFilename != "veil-subscription.txt" || response.RawSubscriptionFilename != "veil-subscription-raw.txt" {
		t.Fatalf("unexpected subscription filenames: base64=%q raw=%q", response.Base64SubscriptionFilename, response.RawSubscriptionFilename)
	}
	if response.SubscriptionContentType != "text/plain; charset=utf-8" {
		t.Fatalf("unexpected subscription content type: %q", response.SubscriptionContentType)
	}
	if got := strings.Join(response.SubscriptionFormats, ","); got != "base64,raw" {
		t.Fatalf("unexpected subscription formats: %q", got)
	}
	if len(response.Links) != 2 {
		t.Fatalf("expected 2 client links, got %+v", response.Links)
	}
	links := map[string]ClientLink{}
	for _, link := range response.Links {
		links[link.Name] = link
	}
	if links["naive"].Protocol != "naiveproxy" || links["naive"].Transport != "tcp" || links["naive"].Port != 443 || links["naive"].URI != "naive+https://veil:naive-secret@vpn.example.com:443" {
		t.Fatalf("unexpected naive link: %+v", links["naive"])
	}
	if links["hysteria2"].Protocol != "hysteria2" || links["hysteria2"].Transport != "udp" || links["hysteria2"].Port != 443 || !strings.HasPrefix(links["hysteria2"].URI, "hysteria2://hy2-secret@vpn.example.com:443/") || !strings.Contains(links["hysteria2"].URI, "sni=vpn.example.com") {
		t.Fatalf("unexpected hysteria2 link: %+v", links["hysteria2"])
	}
}

func TestClientLinksEndpointRequiresDomainAndPasswords(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	req := httptest.NewRequest(http.MethodGet, "/api/client-links", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for incomplete client links config, got %d: %s", w.Code, w.Body.String())
	}
}

func TestClientLinksUsesPerInboundPassword(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := writeRenderableManagementState(statePath, "both"); err != nil {
		t.Fatalf("write state: %v", err)
	}
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})

	// Add a second hysteria2 inbound with its own password via API
	body := strings.NewReader(`{"name":"hysteria2-alt","protocol":"hysteria2","transport":"udp","port":8443,"enabled":true,"password":"alt-hy2-secret"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/inbounds", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Get client links
	req2 := httptest.NewRequest(http.MethodGet, "/api/client-links", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	var response ClientLinksResponse
	if err := json.NewDecoder(w2.Body).Decode(&response); err != nil {
		t.Fatalf("decode client links: %v", err)
	}
	if response.Count != 3 {
		t.Fatalf("expected 3 client links, got %d", response.Count)
	}
	links := map[string]ClientLink{}
	for _, link := range response.Links {
		links[link.Name] = link
	}
	// Original hysteria2 uses global password
	if links["hysteria2"].URI != "hysteria2://hy2-secret@vpn.example.com:443/?sni=vpn.example.com#hysteria2" {
		t.Fatalf("original hysteria2 should use global password, got: %s", links["hysteria2"].URI)
	}
	// New inbound uses its own password
	if links["hysteria2-alt"].URI != "hysteria2://alt-hy2-secret@vpn.example.com:8443/?sni=vpn.example.com#hysteria2-alt" {
		t.Fatalf("new hysteria2 should use per-inbound password, got: %s", links["hysteria2-alt"].URI)
	}
}

func TestClientLinksSubscriptionEndpointReturnsBase64URIs(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := writeRenderableManagementState(statePath, "both"); err != nil {
		t.Fatalf("write state: %v", err)
	}
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})
	req := httptest.NewRequest(http.MethodGet, "/api/client-links/subscription", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Fatalf("unexpected content-type: %q", ct)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("expected no-store cache-control for secret-bearing subscription, got %q", cc)
	}
	if nosniff := w.Header().Get("X-Content-Type-Options"); nosniff != "nosniff" {
		t.Fatalf("expected nosniff for secret-bearing subscription, got %q", nosniff)
	}
	if cd := w.Header().Get("Content-Disposition"); cd != `attachment; filename="veil-subscription.txt"` {
		t.Fatalf("unexpected content-disposition: %q", cd)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(w.Body.String()))
	if err != nil {
		t.Fatalf("decode subscription: %v; body=%q", err, w.Body.String())
	}
	assertClientSubscriptionLines(t, string(decoded))
}

func TestClientLinksSubscriptionEndpointReturnsRawURIs(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := writeRenderableManagementState(statePath, "both"); err != nil {
		t.Fatalf("write state: %v", err)
	}
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})
	req := httptest.NewRequest(http.MethodGet, "/api/client-links/subscription?format=raw", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Fatalf("unexpected content-type: %q", ct)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("expected no-store cache-control for secret-bearing raw subscription, got %q", cc)
	}
	if nosniff := w.Header().Get("X-Content-Type-Options"); nosniff != "nosniff" {
		t.Fatalf("expected nosniff for secret-bearing raw subscription, got %q", nosniff)
	}
	if cd := w.Header().Get("Content-Disposition"); cd != `attachment; filename="veil-subscription-raw.txt"` {
		t.Fatalf("unexpected raw content-disposition: %q", cd)
	}
	if _, err := base64.StdEncoding.DecodeString(strings.TrimSpace(w.Body.String())); err == nil {
		t.Fatalf("raw subscription should not be base64 encoded: %q", w.Body.String())
	}
	assertClientSubscriptionLines(t, w.Body.String())
}

func TestClientLinksSubscriptionEndpointRejectsUnknownFormat(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := writeRenderableManagementState(statePath, "both"); err != nil {
		t.Fatalf("write state: %v", err)
	}
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})
	req := httptest.NewRequest(http.MethodGet, "/api/client-links/subscription?format=json", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assertInvalidSubscriptionFormat(t, w)
}

func TestClientLinksSubscriptionEndpointRejectsUnknownFormatBeforeConfigValidation(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	req := httptest.NewRequest(http.MethodGet, "/api/client-links/subscription?format=json", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assertInvalidSubscriptionFormat(t, w)
}

func TestClientLinksSubscriptionEndpointRejectsUnknownQueryBeforeConfigValidation(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	req := httptest.NewRequest(http.MethodGet, "/api/client-links/subscription?offset=1", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unsupported subscription query, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `unsupported subscription query "offset"`) {
		t.Fatalf("unexpected unsupported query error: %q", w.Body.String())
	}
}
