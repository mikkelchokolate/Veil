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

		if w.Code != http.StatusBadGateway {
			t.Fatalf("expected 502, got %d: %s", w.Code, w.Body.String())
		}
		var result struct {
			Hostname string `json:"hostname"`
			Error    string `json:"error"`
		}
		if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if result.Hostname != "none.such.invalid" {
			t.Errorf("hostname: want none.such.invalid, got %q", result.Hostname)
		}
		if result.Error == "" {
			t.Error("expected non-empty error")
		}
	})

	t.Run("rejects empty hostname", func(t *testing.T) {
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

	t.Run("rejects non-POST", func(t *testing.T) {
		r, _ := NewRouter(ServerInfo{Version: "test"})
		req := httptest.NewRequest(http.MethodGet, "/api/tools/dns-lookup", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", w.Code)
		}
	})
}
