package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStatusCommandRegistered(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"status", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := out.String()
	for _, want := range []string{"--listen", "--auth-token", "--json"} {
		if !strings.Contains(got, want) {
			t.Errorf("help missing %q:\n%s", want, got)
		}
	}
}

func TestFetchStatusJSON(t *testing.T) {
	// Set up a mock sever returning realistic status
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/status" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(statusResponse{
			SchemaVersion: "v1",
			Name:          "Veil",
			Version:       "0.1.0",
			Mode:          "server",
			Services: []serviceStatus{
				{Name: "veil", Managed: true, Unit: "veil.service", ActiveState: "active", SubState: "running"},
				{Name: "naive", Managed: true, Transport: "tcp", Unit: "caddy.service", ActiveState: "active", SubState: "running"},
				{Name: "hysteria2", Managed: true, Transport: "udp", Unit: "hysteria2.service", ActiveState: "active", SubState: "running"},
			},
		})
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"status", "--listen", server.Listener.Addr().String(), "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\noutput: %s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{`"name"`, `"Veil"`, `"version"`, `"0.1.0"`, `"naive"`, `"active"`} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

func TestFetchStatusHumanReadable(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(statusResponse{
			SchemaVersion: "v1",
			Name:          "Veil",
			Version:       "0.1.0",
			Mode:          "server",
			Services: []serviceStatus{
				{Name: "veil", Managed: true, ActiveState: "active"},
				{Name: "naive", Managed: true, Transport: "tcp", ActiveState: "active"},
				{Name: "hysteria2", Managed: true, Transport: "udp", ActiveState: "failed", Error: "connection refused"},
			},
		})
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"status", "--listen", server.Listener.Addr().String()})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := out.String()
	for _, want := range []string{"Veil 0.1.0", "● veil", "● naive", "✕ hysteria2", "connection refused"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

func TestFetchStatusHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("unavailable"))
	}))
	defer server.Close()

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"status", "--listen", server.Listener.Addr().String()})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for 503")
	}
	if !strings.Contains(err.Error(), "503") && !strings.Contains(err.Error(), "Service Unavailable") {
		t.Errorf("expected 503 error, got: %v", err)
	}
	_ = out
}

func TestFetchStatusAuth(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Veil-Token") != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(statusResponse{SchemaVersion: "v1"})
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"status", "--listen", server.Listener.Addr().String(), "--auth-token", "secret", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), `"schemaVersion"`) {
		t.Fatalf("expected JSON response, got: %s", out.String())
	}
}
