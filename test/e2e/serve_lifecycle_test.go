//go:build e2e

package e2e

import (
	"net/http"
	"os"
	"strings"
	"testing"
)

// TestServeHealthLifecycle verifies the readiness endpoint reflects state
// presence: 503 before the state file exists, 200 after a settings write
// persists it.
func TestServeHealthLifecycle(t *testing.T) {
	srv := startServer(t, serverOptions{token: "e2e-secret-token"})

	// /healthz is public (no auth) and unhealthy until state is persisted.
	resp := srv.doNoAuth(http.MethodGet, "/healthz")
	if resp.StatusCode != http.StatusServiceUnavailable {
		drain(resp)
		t.Fatalf("expected 503 before state exists, got %d", resp.StatusCode)
	}
	body := readJSON(t, resp)
	if body["status"] != "unhealthy" {
		t.Fatalf("expected unhealthy status, got %v", body)
	}

	// Persist settings -> state file is created -> health flips to ok.
	resp = srv.do(http.MethodPut, "/api/settings", `{"panelListen":"127.0.0.1:2096","mode":"dev"}`)
	if resp.StatusCode != http.StatusOK {
		body := readJSON(t, resp)
		t.Fatalf("settings write expected 200, got %d: %v", resp.StatusCode, body)
	}
	drain(resp)

	resp = srv.doNoAuth(http.MethodGet, "/healthz")
	if resp.StatusCode != http.StatusOK {
		drain(resp)
		t.Fatalf("expected 200 after state exists, got %d", resp.StatusCode)
	}
	if got := readJSON(t, resp); got["status"] != "ok" {
		t.Fatalf("expected ok status, got %v", got)
	}
}

// TestServeAuthGate verifies bearer-token enforcement on /api/* over a real
// socket: missing token => 401 with WWW-Authenticate, valid token => 200,
// while public routes stay open.
func TestServeAuthGate(t *testing.T) {
	srv := startServer(t, serverOptions{token: "e2e-secret-token"})

	resp := srv.doNoAuth(http.MethodGet, "/api/status")
	if resp.StatusCode != http.StatusUnauthorized {
		drain(resp)
		t.Fatalf("expected 401 without token, got %d", resp.StatusCode)
	}
	if h := resp.Header.Get("WWW-Authenticate"); !strings.Contains(h, "Bearer") {
		t.Fatalf("expected Bearer challenge header, got %q", h)
	}
	drain(resp)

	// Valid token => 200.
	req := srv.do(http.MethodGet, "/api/status", "")
	if req.StatusCode != http.StatusOK {
		drain(req)
		t.Fatalf("expected 200 with valid token, got %d", req.StatusCode)
	}
	drain(req)
}

// TestServeAuthDisabled verifies that with no token configured, /api/* is
// reachable without credentials and the startup log says auth is disabled.
func TestServeAuthDisabled(t *testing.T) {
	srv := startServer(t, serverOptions{
		seedState: `{"settings":{"panelListen":"127.0.0.1:2096","mode":"dev"}}`,
	})

	resp := srv.doNoAuth(http.MethodGet, "/api/status")
	if resp.StatusCode != http.StatusOK {
		t.Logf("Server logs:\n%s", srv.logBuf.String())
		drain(resp)
		t.Fatalf("expected 200 with auth disabled, got %d", resp.StatusCode)
	}
	drain(resp)

	logs := srv.gracefulShutdown()
	if !strings.Contains(logs, "API auth: disabled") {
		t.Fatalf("expected 'API auth: disabled' in logs, got:\n%s", logs)
	}
}

// TestGracefulShutdownExitsClean verifies SIGINT triggers a clean drain and
// exit-0 with the expected lifecycle log lines.
func TestGracefulShutdownExitsClean(t *testing.T) {
	srv := startServer(t, serverOptions{token: "tok"})
	resp := srv.doNoAuth(http.MethodGet, "/healthz")
	drain(resp)
	logs := srv.gracefulShutdown()
	for _, want := range []string{"Shutting down", "Server stopped"} {
		if !strings.Contains(logs, want) {
			t.Fatalf("expected %q in shutdown logs, got:\n%s", want, logs)
		}
	}
}

// TestStatePersistsAcrossRestart verifies that configuration written through
// one serve process is durable: a second process started against the same
// state file serves the same inbound.
func TestStatePersistsAcrossRestart(t *testing.T) {
	srv := startServer(t, serverOptions{token: "tok"})
	resp := srv.do(http.MethodPut, "/api/settings", `{"panelListen":"127.0.0.1:2096","mode":"dev"}`)
	drain(resp)
	resp = srv.do(http.MethodPost, "/api/inbounds", `{"name":"persist-me","protocol":"mieru","transport":"tcp","port":9443,"enabled":true,"password":"pw"}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("inbound expected 201, got %d", resp.StatusCode)
	}
	drain(resp)
	statePath := srv.statePath
	srv.gracefulShutdown()

	// Read the seed from the now-stopped server's state file and relaunch.
	seed, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	srv2 := startServer(t, serverOptions{token: "tok", seedState: string(seed)})
	resp = srv2.do(http.MethodGet, "/api/inbounds", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("inbounds expected 200, got %d", resp.StatusCode)
	}
	raw, _ := os.ReadFile(srv2.statePath)
	if !strings.Contains(string(raw), "persist-me") {
		t.Fatalf("expected persisted inbound 'persist-me' after restart in %s", srv2.statePath)
	}
}
