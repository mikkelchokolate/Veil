package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestSpeedtestEndpointRejectsInvalidContentType(t *testing.T) {
	handler, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: filepath.Join(t.TempDir(), "state.json")})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	res, err := http.Post(srv.URL+"/api/tools/speedtest", "text/plain", strings.NewReader("bad"))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("expected 415, got %d", res.StatusCode)
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
	settingsBody := strings.NewReader(`{"panelListen":"127.0.0.1:2096","mode":"server"}`)
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
		if len(result.Rules) != 2 {
			t.Fatalf("expected panel + TLS-ALPN-01 rules, got %d: %+v", len(result.Rules), result.Rules)
		}
		hasPanel := false
		hasTLSALPN := false
		for _, rule := range result.Rules {
			switch {
			case rule.Port == 2096 && rule.Protocol == "tcp":
				hasPanel = true
			case rule.Port == 443 && rule.Protocol == "tcp":
				hasTLSALPN = true
			}
		}
		if !hasPanel {
			t.Error("missing panel TCP/2096 rule")
		}
		if !hasTLSALPN {
			t.Error("missing TLS-ALPN-01 TCP/443 rule")
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
