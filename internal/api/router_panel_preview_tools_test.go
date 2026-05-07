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

var _routerTestDeps_router_panel_preview_tools_test = []any{
	bytes.Buffer{}, sha256.Sum256, tls.VersionTLS12, base64.StdEncoding, hex.EncodeToString, json.NewDecoder, errors.New, fmt.Sprintf, log.Printf, http.MethodGet, httptest.NewRecorder, os.ReadFile, filepath.Join, strings.Contains, testing.T{},
}

func TestRouterServesPanelShell(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			r, _ := NewRouter(ServerInfo{Version: "test"})
			req := httptest.NewRequest(method, "/", nil)
			w := httptest.NewRecorder()

			r.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", w.Code)
			}
			if method == http.MethodHead && w.Body.Len() != 0 {
				t.Fatalf("expected empty HEAD body, got %q", w.Body.String())
			}
			if ct := w.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
				t.Fatalf("unexpected content-type: %q", ct)
			}
			if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
				t.Fatalf("expected no-store cache-control for token-bearing panel shell, got %q", cc)
			}
			if pragma := w.Header().Get("Pragma"); pragma != "no-cache" {
				t.Fatalf("expected no-cache Pragma for token-bearing panel shell, got %q", pragma)
			}
			if nosniff := w.Header().Get("X-Content-Type-Options"); nosniff != "nosniff" {
				t.Fatalf("expected nosniff for panel shell, got %q", nosniff)
			}
			if csp := w.Header().Get("Content-Security-Policy"); csp != "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'" {
				t.Fatalf("unexpected panel content-security-policy: %q", csp)
			}
			if referrer := w.Header().Get("Referrer-Policy"); referrer != "no-referrer" {
				t.Fatalf("unexpected panel referrer-policy: %q", referrer)
			}
			if xfo := w.Header().Get("X-Frame-Options"); xfo != "DENY" {
				t.Fatalf("unexpected panel x-frame-options: %q", xfo)
			}
			if permissions := w.Header().Get("Permissions-Policy"); permissions != "camera=(), microphone=(), geolocation=(), payment=(), usb=()" {
				t.Fatalf("unexpected panel permissions-policy: %q", permissions)
			}
			if coop := w.Header().Get("Cross-Origin-Opener-Policy"); coop != "same-origin" {
				t.Fatalf("unexpected panel cross-origin-opener-policy: %q", coop)
			}
			if corp := w.Header().Get("Cross-Origin-Resource-Policy"); corp != "same-origin" {
				t.Fatalf("unexpected panel cross-origin-resource-policy: %q", corp)
			}
			if oac := w.Header().Get("Origin-Agent-Cluster"); oac != "?1" {
				t.Fatalf("unexpected panel origin-agent-cluster: %q", oac)
			}
			if method == http.MethodGet {
				if body := w.Body.String(); !strings.Contains(body, "Veil Panel") || !strings.Contains(body, "/api/version") || !strings.Contains(body, "/api/status") || !strings.Contains(body, "/api/firewall") || !strings.Contains(body, "/api/tools/dns-lookup") || !strings.Contains(body, "/api/tools/ping") || !strings.Contains(body, "/api/apply/plan") || !strings.Contains(body, "/api/apply") || !strings.Contains(body, "Apply staged files") || !strings.Contains(body, "Apply live configs") || !strings.Contains(body, "Reload and health check services") || !strings.Contains(body, "Load apply history") || !strings.Contains(body, "Service status") || !strings.Contains(body, "loadServiceStatus") || !strings.Contains(body, "Client links") || !strings.Contains(body, "/api/client-links") || !strings.Contains(body, "/api/client-links/subscription") || !strings.Contains(body, "format=base64") || !strings.Contains(body, "format=raw") || !strings.Contains(body, "copy-client-links") || !strings.Contains(body, "copyClientLinksOutput") || !strings.Contains(body, "navigator.clipboard.writeText") || !strings.Contains(body, "download-client-subscription") || !strings.Contains(body, "download-client-subscription-raw") || !strings.Contains(body, "downloadClientSubscriptionPath") || !strings.Contains(body, "URL.createObjectURL") || !strings.Contains(body, "veil-subscription-raw.txt") {
					t.Fatalf("unexpected panel body: %s", body)
				}
			}
		})
	}
}

func TestRouterServesPanelShellWithApplyHistoryFilters(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	body := w.Body.String()
	for _, want := range []string{
		"apply-history-stage",
		"apply-history-success",
		"apply-history-limit",
		"loadApplyHistory",
		"/api/apply/history?",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("panel shell missing apply history filter control %q: %s", want, body)
		}
	}
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

func TestRURecommendedPreviewEndpoint(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
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
	encoded, _ := json.Marshal(response)
	if strings.Contains(string(encoded), "preview-naive") || strings.Contains(string(encoded), "preview-hysteria2") || strings.Contains(string(encoded), "preview-panel") {
		t.Fatalf("preview response leaked generated secrets: %s", string(encoded))
	}
	if !strings.Contains(string(encoded), "[REDACTED]") {
		t.Fatalf("preview response should include redaction markers: %s", string(encoded))
	}
}

func TestRURecommendedPreviewEndpointHonorsStack(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
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
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
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

	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
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

	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	req := httptest.NewRequest(http.MethodPost, "/api/tools/speedtest", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRouterServesPanelShellWithSpeedtestControl(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "Speedtest") || !strings.Contains(body, "/api/tools/speedtest") {
		t.Fatalf("expected speedtest control in panel shell: %s", body)
	}
}

func TestRouterServesPanelShellWithManagementSections(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	body := w.Body.String()
	for _, want := range []string{"Settings", "Inbounds", "Routing rules", "WARP", "/api/settings", "/api/inbounds", "/api/routing/rules", "/api/warp"} {
		if !strings.Contains(body, want) {
			t.Fatalf("panel shell missing %q: %s", want, body)
		}
	}
}

func TestRouterServesPanelShellWithTokenControls(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	body := w.Body.String()
	for _, want := range []string{"API token", "localStorage", "veil_api_token", "X-Veil-Token"} {
		if !strings.Contains(body, want) {
			t.Fatalf("panel shell missing auth control %q: %s", want, body)
		}
	}
}

func TestRouterServesPanelShellWithManagementForms(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	body := w.Body.String()
	for _, want := range []string{
		"settings-form",
		"settings-domain",
		"settings-naive-password",
		"saveSettings",
		"loadSettingsIntoForm",
		"inbound-form",
		"inbound-name",
		"inbound-protocol",
		"inbound-transport",
		"saveInbound",
		"deleteInbound",
		"routing-rule-form",
		"routing-rule-name",
		"routing-rule-match",
		"routing-rule-outbound",
		"routing-preset-profile",
		"applyRoutingPreset",
		"/api/routing/presets",
		"saveRoutingRule",
		"deleteRoutingRule",
		"warp-form",
		"warp-enabled",
		"warp-private-key",
		"warp-local-address",
		"warp-peer-public-key",
		"saveWarpConfig",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("panel shell missing management form control %q: %s", want, body)
		}
	}
}

func TestSpeedtestEndpointRejectsInvalidContentType(t *testing.T) {
	handler, _ := NewRouter(ServerInfo{Version: "test", StatePath: "/dev/null"})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	res, err := http.Post(srv.URL+"/api/tools/speedtest", "text/plain", strings.NewReader("bad"))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", res.StatusCode)
	}
}

func TestDNSLookupEndpoint(t *testing.T) {
	orig := dnsLookuper
	defer func() { dnsLookuper = orig }()

	t.Run("POST resolves hostname", func(t *testing.T) {
		r, _ := NewRouter(ServerInfo{Version: "test"})
		dnsLookuper = func(host string) ([]string, string, error) {
			return []string{"93.184.216.34", "2606:2800:220:1:248:1893:25c8:1946"}, "example.com.", nil
		}
		body := strings.NewReader(`{"hostname":"example.com"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/tools/dns-lookup", body)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var result struct {
			Hostname  string   `json:"hostname"`
			Addresses []string `json:"addresses"`
			CNAME     string   `json:"cname,omitempty"`
			Error     string   `json:"error,omitempty"`
		}
		if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if result.Hostname != "example.com" {
			t.Errorf("hostname: want example.com, got %q", result.Hostname)
		}
		if len(result.Addresses) != 2 {
			t.Fatalf("expected 2 addresses, got %d: %v", len(result.Addresses), result.Addresses)
		}
		if result.Addresses[0] != "93.184.216.34" {
			t.Errorf("addresses[0]: want 93.184.216.34, got %q", result.Addresses[0])
		}
		if result.CNAME != "example.com." {
			t.Errorf("cname: want example.com., got %q", result.CNAME)
		}
	})

	t.Run("POST returns error for NXDOMAIN", func(t *testing.T) {
		r, _ := NewRouter(ServerInfo{Version: "test"})
		dnsLookuper = func(host string) ([]string, string, error) {
			return nil, "", errors.New("lookup none.such.invalid: no such host")
		}
		body := strings.NewReader(`{"hostname":"none.such.invalid"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/tools/dns-lookup", body)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var result struct {
			Hostname  string   `json:"hostname"`
			Addresses []string `json:"addresses"`
			Error     string   `json:"error,omitempty"`
		}
		if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if result.Error == "" {
			t.Error("expected error for NXDOMAIN")
		}
	})

	t.Run("POST rejects missing hostname", func(t *testing.T) {
		r, _ := NewRouter(ServerInfo{Version: "test"})
		body := strings.NewReader(`{}`)
		req := httptest.NewRequest(http.MethodPost, "/api/tools/dns-lookup", body)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("POST rejects empty hostname", func(t *testing.T) {
		r, _ := NewRouter(ServerInfo{Version: "test"})
		body := strings.NewReader(`{"hostname":""}`)
		req := httptest.NewRequest(http.MethodPost, "/api/tools/dns-lookup", body)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("GET returns method not allowed", func(t *testing.T) {
		r, _ := NewRouter(ServerInfo{Version: "test"})
		req := httptest.NewRequest(http.MethodGet, "/api/tools/dns-lookup", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", w.Code)
		}
	})
}

func TestFirewallEndpoint(t *testing.T) {
	orig := firewallStatusReader
	defer func() { firewallStatusReader = orig }()

	r, _ := NewRouter(ServerInfo{Version: "test"})

	// Configure settings with a panel port
	settingsBody := strings.NewReader(`{"panelListen":"127.0.0.1:2096","stack":"both","mode":"server"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", settingsBody)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT /api/settings returned %d: %s", w.Code, w.Body.String())
	}

	t.Run("GET returns firewall plan with UFW active", func(t *testing.T) {
		firewallStatusReader = func() (bool, error) { return true, nil }
		req := httptest.NewRequest(http.MethodGet, "/api/firewall", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var result struct {
			Active bool `json:"active"`
			Rules  []struct {
				Port     int    `json:"port"`
				Protocol string `json:"protocol"`
				Service  string `json:"service"`
			} `json:"rules"`
		}
		if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if !result.Active {
			t.Error("expected UFW active")
		}
		if len(result.Rules) != 3 {
			t.Fatalf("expected 3 rules, got %d: %+v", len(result.Rules), result.Rules)
		}
		// Default inbounds: naive:443/tcp, hysteria2:443/udp, panel:2096/tcp
		hasTCP := false
		hasUDP := false
		hasPanel := false
		for _, rule := range result.Rules {
			switch {
			case rule.Port == 443 && rule.Protocol == "tcp":
				hasTCP = true
			case rule.Port == 443 && rule.Protocol == "udp":
				hasUDP = true
			case rule.Port == 2096 && rule.Protocol == "tcp":
				hasPanel = true
			}
		}
		if !hasTCP {
			t.Error("missing NaiveProxy TCP/443 rule")
		}
		if !hasUDP {
			t.Error("missing Hysteria2 UDP/443 rule")
		}
		if !hasPanel {
			t.Error("missing panel TCP/2096 rule")
		}
	})

	t.Run("GET returns firewall plan with UFW inactive", func(t *testing.T) {
		firewallStatusReader = func() (bool, error) { return false, nil }
		req := httptest.NewRequest(http.MethodGet, "/api/firewall", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var result struct {
			Active bool `json:"active"`
		}
		if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if result.Active {
			t.Error("expected UFW inactive")
		}
	})

	t.Run("HEAD returns headers with empty body", func(t *testing.T) {
		firewallStatusReader = func() (bool, error) { return true, nil }
		req := httptest.NewRequest(http.MethodHead, "/api/firewall", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		if w.Body.Len() != 0 {
			t.Fatalf("expected empty HEAD body, got %q", w.Body.String())
		}
	})

	t.Run("POST returns method not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/firewall", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", w.Code)
		}
	})
}

func TestPingEndpoint(t *testing.T) {
	orig := pingRunner
	defer func() { pingRunner = orig }()

	t.Run("POST pings successfully", func(t *testing.T) {
		r, _ := NewRouter(ServerInfo{Version: "test"})
		pingRunner = func(host string, count int) PingResult {
			return PingResult{
				Host:        host,
				Transmitted: count,
				Received:    count,
				LossPct:     0,
				MinMs:       1.5,
				AvgMs:       2.3,
				MaxMs:       4.1,
				StddevMs:    0.8,
			}
		}
		body := strings.NewReader(`{"host":"8.8.8.8","count":4}`)
		req := httptest.NewRequest(http.MethodPost, "/api/tools/ping", body)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var result PingResult
		if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if result.Host != "8.8.8.8" {
			t.Errorf("host: want 8.8.8.8, got %q", result.Host)
		}
		if result.Transmitted != 4 {
			t.Errorf("transmitted: want 4, got %d", result.Transmitted)
		}
		if result.Received != 4 {
			t.Errorf("received: want 4, got %d", result.Received)
		}
		if result.LossPct != 0 {
			t.Errorf("lossPct: want 0, got %f", result.LossPct)
		}
	})

	t.Run("POST pings with packet loss", func(t *testing.T) {
		r, _ := NewRouter(ServerInfo{Version: "test"})
		pingRunner = func(host string, count int) PingResult {
			return PingResult{
				Host:        host,
				Transmitted: 3,
				Received:    1,
				LossPct:     66.67,
				MinMs:       10.0,
				AvgMs:       10.0,
				MaxMs:       10.0,
			}
		}
		body := strings.NewReader(`{"host":"192.168.1.1"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/tools/ping", body)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var result PingResult
		json.NewDecoder(w.Body).Decode(&result)
		if result.Received != 1 {
			t.Errorf("received: want 1, got %d", result.Received)
		}
		if result.LossPct < 66 {
			t.Errorf("lossPct: want ~66.67, got %f", result.LossPct)
		}
	})

	t.Run("POST rejects missing host", func(t *testing.T) {
		r, _ := NewRouter(ServerInfo{Version: "test"})
		body := strings.NewReader(`{"count":3}`)
		req := httptest.NewRequest(http.MethodPost, "/api/tools/ping", body)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("POST rejects empty host", func(t *testing.T) {
		r, _ := NewRouter(ServerInfo{Version: "test"})
		body := strings.NewReader(`{"host":""}`)
		req := httptest.NewRequest(http.MethodPost, "/api/tools/ping", body)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("POST rejects count above 10", func(t *testing.T) {
		r, _ := NewRouter(ServerInfo{Version: "test"})
		body := strings.NewReader(`{"host":"8.8.8.8","count":20}`)
		req := httptest.NewRequest(http.MethodPost, "/api/tools/ping", body)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("GET returns method not allowed", func(t *testing.T) {
		r, _ := NewRouter(ServerInfo{Version: "test"})
		req := httptest.NewRequest(http.MethodGet, "/api/tools/ping", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", w.Code)
		}
	})
}
