package caddyadmin

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/runtimeports"
)

func TestLoadConfigRejectsAdminStateWithDifferentDigest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/load":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.Path == "/config/":
			_, _ = w.Write([]byte(`{"apps":{"http":{}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	err := NewClient(srv.URL).LoadConfig([]byte(`{"apps":{}}`))
	if err == nil {
		t.Fatal("HTTP 200 was accepted without proving active Caddy config digest")
	}
	// The failure must be the digest mismatch, not a transport/decode error.
	if !strings.Contains(err.Error(), "caddy active config digest mismatch") {
		t.Fatalf("expected digest mismatch error, got %v", err)
	}
	if !strings.Contains(err.Error(), "expected") || !strings.Contains(err.Error(), "observed") {
		t.Fatalf("digest mismatch error must name expected/observed digests, got %v", err)
	}
}

func TestLoadConfigPostsJSON(t *testing.T) {
	var received, contentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			if r.URL.Path != "/load" {
				t.Errorf("path = %s", r.URL.Path)
			}
			contentType = r.Header.Get("Content-Type")
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read body: %v", err)
			}
			received = string(body)
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			if r.URL.Path != "/config/" {
				t.Errorf("verify path = %s", r.URL.Path)
			}
			_, _ = w.Write([]byte(received))
		default:
			t.Errorf("method = %s", r.Method)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	if err := c.LoadConfig([]byte(`{"apps":{}}`)); err != nil {
		t.Fatal(err)
	}
	// Caddy's /load endpoint requires application/json and the exact payload —
	// a non-empty body alone would green a mangled or re-encoded post (#885).
	if contentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", contentType)
	}
	if received != `{"apps":{}}` {
		t.Errorf("body = %q, want %q", received, `{"apps":{}}`)
	}
}

// serveUnixAdmin starts an Admin-API stand-in on a Unix socket that enforces
// the same Host rule Caddy 2.11 applies to unix-socket admin listeners: only
// a loopback Host (or none) is accepted. A client that leaked the socket path
// or a TCP authority into Host would get Caddy's 403 "host not allowed".
func serveUnixAdmin(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "caddyadm")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "admin.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Skipf("unix sockets unavailable: %v", err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Host {
		case "", "127.0.0.1", "::1", "localhost":
		default:
			http.Error(w, `{"error":"host not allowed: `+r.Host+`"}`, http.StatusForbidden)
			return
		}
		handler(w, r)
	})}
	go func() { _ = srv.Serve(listener) }()
	t.Cleanup(func() { _ = srv.Close() })
	return socket
}

// #1276: the admin API moved from loopback TCP (reachable by every local
// uid) to an owner-only Unix socket. The client must dial the socket named
// by a Caddy-style "unix/<path>" endpoint, ignore the "|<mode>" permission
// suffix Caddy's listen syntax allows, and still prove the active digest.
func TestLoadConfigOverUnixSocketEndpoint(t *testing.T) {
	var received string
	socket := serveUnixAdmin(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/load":
			body, _ := io.ReadAll(r.Body)
			received = string(body)
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.Path == "/config/":
			_, _ = w.Write([]byte(received))
		default:
			http.NotFound(w, r)
		}
	})
	for _, endpoint := range []string{"unix/" + socket, "unix/" + socket + "|0200"} {
		received = ""
		// A caller-supplied client (the helper passes one with its own
		// timeout) must not redirect the dial back onto TCP.
		c := NewClient(endpoint)
		c.HTTPClient = &http.Client{Timeout: 5 * time.Second}
		if err := c.LoadConfig([]byte(`{"apps":{}}`)); err != nil {
			t.Fatalf("%s: %v", endpoint, err)
		}
		if received != `{"apps":{}}` {
			t.Fatalf("%s: body = %q", endpoint, received)
		}
	}
}

func TestLoadConfigUnixSocketMissingFailsClosed(t *testing.T) {
	dir := t.TempDir()
	err := NewClient("unix/" + filepath.Join(dir, "absent.sock")).LoadConfig([]byte(`{"apps":{}}`))
	if err == nil || !strings.Contains(err.Error(), "caddy admin POST failed") {
		t.Fatalf("missing admin socket must fail the load, got %v", err)
	}
}

func TestNewClientDefaultsToAdminUnixSocket(t *testing.T) {
	c := NewClient("")
	if c.AdminEndpoint != "unix/"+runtimeports.CaddyAdminSocketPath {
		t.Fatalf("default endpoint = %q, want unix/%s", c.AdminEndpoint, runtimeports.CaddyAdminSocketPath)
	}
}
