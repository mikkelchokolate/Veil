//go:build e2e

package e2e

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// TestPortCollisionPanel verifies that starting the serve command on a port
// that is already bound by another process fails cleanly.
func TestPortCollisionPanel(t *testing.T) {
	// 1. Bind to a free port to simulate collision
	port := freePort(t)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("setup mock collision listener: %v", err)
	}
	defer ln.Close()

	// 2. Attempt to start veil server on the same address
	bin := veilBinary(t)
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	applyRoot := filepath.Join(dir, "apply")
	keyPath := filepath.Join(dir, "state.key")

	cmd := execCommand(bin, "serve")
	cmd.Env = append(os.Environ(),
		"VEIL_LISTEN="+addr,
		"VEIL_STATE_PATH="+statePath,
		"VEIL_APPLY_ROOT="+applyRoot,
		"VEIL_KEY_PATH="+keyPath,
	)

	logBuf := &syncBuffer{}
	cmd.Stdout = logBuf
	cmd.Stderr = logBuf

	if err := cmd.Start(); err != nil {
		// A failure to exec the binary is a harness failure, not evidence the
		// port collision was handled — fail rather than silently passing.
		t.Fatalf("veil serve failed to start: %v", err)
	}

	// 3. The process should exit with an error because the port is in use
	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()

	select {
	case err := <-waitErr:
		if err == nil {
			t.Fatalf("expected server to exit with bind error, but exited with code 0. Logs:\n%s", logBuf.String())
		}
		// The exit must be caused by the bind failure, not some unrelated
		// startup error — assert the log names the address-in-use condition.
		logs := logBuf.String()
		if !strings.Contains(logs, "address already in use") && !strings.Contains(logs, "Only one usage of each socket address") {
			t.Fatalf("server exited non-zero but not with a bind error. Logs:\n%s", logs)
		}
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("server hung on port collision and did not exit within 5s. Logs:\n%s", logBuf.String())
	}
}

// TestPortCollisionInbound verifies that setting up an inbound port that
// is already in use by another process causes the apply command to detect the service
// restart failure and correctly report or handle the conflict.
func TestPortCollisionInbound(t *testing.T) {
	srv := startServer(t, serverOptions{token: "e2e-secret-token"})
	inboundPort := freePort(t)

	// Setup settings
	resp := srv.do(http.MethodPut, "/api/settings", `{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"vpn.example.com"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("settings setup failed: %d", resp.StatusCode)
	}
	drain(resp)

	// Bind to a free port to create a collision for the inbound
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", inboundPort))
	if err != nil {
		t.Fatalf("failed to bind mock inbound port: %v", err)
	}
	defer ln.Close()

	// Try to add inbound on the same port
	resp = srv.do(http.MethodPost, "/api/inbounds", fmt.Sprintf(`{"name":"mieru-tcp-coll","protocol":"mieru","transport":"tcp","port":%d,"enabled":true,"password":"pass"}`, inboundPort))
	// Live validation rejects a host-level collision before state mutation.
	if resp.StatusCode == http.StatusUnprocessableEntity {
		body := readJSON(t, resp)
		if !jsonContainsIssue(body, "port_in_use") {
			t.Fatalf("422 response missing port_in_use issue: %+v", body)
		}
		return
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created or 422 Unprocessable Entity, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)

	// Build apply plan
	resp = srv.do(http.MethodPost, "/api/apply/plan", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("plan expected 200, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)

	// Apply. Since systemd is not present on Windows, the service reload step will fail.
	// We verify that the API returns a failure/conflict or correctly rolls back.
	resp = srv.do(http.MethodPost, "/api/apply", `{"confirm":true,"applyLive":true,"applyServices":true}`)
	body := readJSON(t, resp)
	if resp.StatusCode == http.StatusOK {
		// If it succeeded, verify if the service actions report failure/rollback because of the missing systemd or collision
		// Wait, if systemd reload fails, the code returns 400 Bad Request with rollback status.
		// If it's Windows, reload fails, so it should roll back.
		if rolledBack, ok := body["rolledBack"].(bool); !ok || !rolledBack {
			t.Fatalf("expected reload failure to trigger rollback on port collision, got: %+v", body)
		}
	} else if resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected apply error or rollback, got %d: %+v", resp.StatusCode, body)
	}
}

// TestBadAuthentication validates rejections for invalid tokens, invalid
// cookies, and CSRF enforcement on a REAL session: a valid session cookie
// without (or with a wrong) CSRF token must get exactly 403 on a mutation,
// while the same session with the correct CSRF token succeeds.
func TestBadAuthentication(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("e2e-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	srv := startServer(t, serverOptions{
		token: "e2e-secret-token",
		seedState: `{"schemaVersion":4,` +
			`"setup":{"completed":true},` +
			`"settings":{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"vpn.example.com"},` +
			`"users":[{"username":"e2e-admin","passwordHash":"` + string(passwordHash) + `","role":"admin"}]}`,
	})
	sessionsPath := filepath.Join(filepath.Dir(srv.statePath), "sessions.json")
	if err := os.WriteFile(sessionsPath, []byte(`{"version":1,"sessions":[]}`), 0o600); err != nil {
		t.Fatalf("seed empty sessions store: %v", err)
	}

	// 1. Invalid Bearer Token -> 401
	req, _ := http.NewRequest(http.MethodGet, srv.baseURL+"/api/status", nil)
	req.Header.Set("Authorization", "Bearer invalid-token-value")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for invalid Bearer token, got %d", resp.StatusCode)
	}
	drain(resp)

	// 2. Invalid Session Cookie -> 401
	req, _ = http.NewRequest(http.MethodGet, srv.baseURL+"/api/status", nil)
	req.AddCookie(&http.Cookie{Name: "veil_session", Value: "invalid-session-id"})
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for invalid session cookie, got %d", resp.StatusCode)
	}
	drain(resp)

	// 3. Real login to obtain a VALID session cookie + its CSRF token.
	req, _ = http.NewRequest(http.MethodPost, srv.baseURL+"/api/auth/login",
		strings.NewReader(`{"username":"e2e-admin","password":"e2e-password"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("login request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login failed: %d %v", resp.StatusCode, readJSON(t, resp))
	}
	var sessionCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "veil_session" {
			sessionCookie = c
		}
	}
	loginBody := readJSON(t, resp)
	if sessionCookie == nil || sessionCookie.Value == "" {
		t.Fatalf("login did not set veil_session cookie: %+v", resp.Header)
	}
	csrfToken, _ := loginBody["csrfToken"].(string)
	if csrfToken == "" {
		t.Fatalf("login response missing csrfToken: %v", loginBody)
	}

	settingsBody := `{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"vpn.example.com"}`
	putSettings := func(withCSRF string) *http.Response {
		req, _ := http.NewRequest(http.MethodPut, srv.baseURL+"/api/settings", strings.NewReader(settingsBody))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(sessionCookie)
		if withCSRF != "" {
			req.Header.Set("X-CSRF-Token", withCSRF)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("settings request failed: %v", err)
		}
		return resp
	}

	// 3a. Valid session cookie, NO CSRF header -> exactly 403.
	resp = putSettings("")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("valid session without CSRF: expected 403, got %d (%v)", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)

	// 3b. Valid session cookie, WRONG CSRF token -> exactly 403.
	resp = putSettings("invalid-csrf-token")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("valid session with wrong CSRF: expected 403, got %d (%v)", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)

	// 3c. Valid session cookie + the real CSRF token -> the mutation succeeds.
	resp = putSettings(csrfToken)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("valid session with correct CSRF: expected 200, got %d (%v)", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)

	// 3d. The static bearer token is not a cookie session, so CSRF does not
	// apply to it — the same mutation succeeds without any CSRF header.
	resp = srv.do(http.MethodPut, "/api/settings", settingsBody)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bearer-token mutation without CSRF: expected 200, got %d (%v)", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)
}

// TestCorruptedStateRecovery verifies that the CLI validate command fails
// gracefully with an exit error when state.json contains corrupted JSON data.
func TestCorruptedStateRecovery(t *testing.T) {
	dir := t.TempDir()
	badJSON := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(badJSON, []byte(`{invalid-json-content: {`), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := runCLI(t, nil, "config", "validate", "--state", badJSON)
	if err == nil {
		t.Fatalf("expected validation error on corrupted JSON, but succeeded. Output:\n%s", out)
	}
	if !strings.Contains(strings.ToLower(out), "invalid json") && !strings.Contains(strings.ToLower(out), "syntax") {
		t.Fatalf("expected output to mention JSON syntax/invalid error, got: %s", out)
	}
}

// Helper definitions matching harness_test.go to compile and execute commands
func execCommand(name string, arg ...string) *exec.Cmd {
	return exec.Command(name, arg...)
}
