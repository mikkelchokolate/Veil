//go:build e2e

package e2e

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFullInboundToApplyFlow drives the complete management lifecycle over a
// real socket: configure settings, create two Mieru inbounds, fetch client
// links, then stage an apply and confirm generated config artifacts land on
// disk under the apply root.
func TestFullInboundToApplyFlow(t *testing.T) {
	srv := startServer(t, serverOptions{token: "e2e-secret-token"})

	resp := srv.do(http.MethodPut, "/api/settings", `{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"vpn.example.com"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("settings expected 200, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)

	resp = srv.do(http.MethodPost, "/api/inbounds", `{"name":"mieru-tcp","protocol":"mieru","transport":"tcp","port":8443,"enabled":true,"profiles":[{"name":"alice","password":"alice-pass","enabled":true}]}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("inbound 1 expected 201, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)

	resp = srv.do(http.MethodPost, "/api/inbounds", `{"name":"mieru-udp","protocol":"mieru","transport":"udp","port":8443,"enabled":true,"password":"udp-pass"}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("inbound 2 expected 201, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)

	// Client links should aggregate the enabled Mieru transports.
	resp = srv.do(http.MethodGet, "/api/client-links", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("client links expected 200, got %d", resp.StatusCode)
	}
	links := readJSON(t, resp)
	if count, ok := links["count"].(float64); !ok || count < 1 {
		t.Fatalf("expected at least one client link, got %v", links["count"])
	}

	// Plan, then apply; expect generated mieru config under the apply root.
	resp = srv.do(http.MethodPost, "/api/apply/plan", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("apply plan expected 200, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)

	resp = srv.do(http.MethodPost, "/api/apply", `{"confirm":true}`)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusConflict {
		t.Fatalf("apply expected 200/409, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)

	// A generated mieru config artifact should exist somewhere under the
	// apply root after staging.
	found := false
	_ = filepath.Walk(srv.applyRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.Contains(strings.ToLower(path), "mieru") {
			found = true
		}
		return nil
	})
	if !found {
		t.Fatalf("expected a generated mieru artifact under apply root %s", srv.applyRoot)
	}
}

// TestRejectsDuplicateMieruUsernamesEndToEnd confirms the aggregation
// safeguard (duplicate user names across enabled Mieru inbounds) surfaces as
// an apply/plan error over the real HTTP surface rather than silently writing
// a broken config.
func TestRejectsDuplicateMieruUsernamesEndToEnd(t *testing.T) {
	srv := startServer(t, serverOptions{token: "tok"})

	resp := srv.do(http.MethodPut, "/api/settings", `{"panelListen":"127.0.0.1:2096","mode":"dev"}`)
	drain(resp)

	// Two Mieru inbounds whose sole client profile shares the same name.
	resp = srv.do(http.MethodPost, "/api/inbounds", `{"name":"dup","protocol":"mieru","transport":"tcp","port":8443,"enabled":true,"password":"pw1"}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("inbound 1 expected 201, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)
	resp = srv.do(http.MethodPost, "/api/inbounds", `{"name":"dup","protocol":"mieru","transport":"udp","port":8443,"enabled":true,"password":"pw2"}`)
	// The catalog may reject the duplicate name at creation, or the conflict
	// may surface at plan/apply time. Either is an acceptable safeguard; what
	// matters is that it never silently succeeds end-to-end.
	if resp.StatusCode == http.StatusCreated {
		drain(resp)
		planResp := srv.do(http.MethodPost, "/api/apply/plan", "")
		planBody := readJSON(t, planResp)
		if planResp.StatusCode == http.StatusOK {
			// If plan is OK, apply must fail with the duplicate-user error.
			applyResp := srv.do(http.MethodPost, "/api/apply", `{"confirm":true}`)
			if applyResp.StatusCode == http.StatusOK {
				t.Fatalf("expected duplicate mieru user names to be rejected, but apply succeeded: %v", planBody)
			}
			drain(applyResp)
		}
		return
	}
	if resp.StatusCode < 400 {
		t.Fatalf("expected duplicate inbound to be rejected, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)
}

// TestConfigValidateCLI exercises the `veil config validate` subcommand
// against the real binary with both a valid and an invalid state file.
func TestConfigValidateCLI(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.json")
	if err := os.WriteFile(good, []byte(`{"settings":{"panelListen":"127.0.0.1:2096","mode":"dev"},"inbounds":[],"routingRules":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := runCLI(t, nil, "config", "validate", "--state", good)
	if err != nil {
		t.Fatalf("valid state should pass, got err=%v out=%s", err, out)
	}

	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte(`{"settings":{"panelListen":"127.0.0.1:2096","mode":"dev"},"inbounds":[{"name":"x","protocol":"mieru","transport":"tcp","port":70000}],"routingRules":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err = runCLI(t, nil, "config", "validate", "--state", bad)
	if err == nil {
		t.Fatalf("invalid state should fail validation, out=%s", out)
	}
}

// TestVersionAndDoctorCLI smoke-tests two read-only subcommands end-to-end.
func TestVersionAndDoctorCLI(t *testing.T) {
	out, err := runCLI(t, nil, "version")
	if err != nil {
		t.Fatalf("version failed: %v (%s)", err, out)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatal("version produced no output")
	}

	// doctor exits non-zero when required commands are missing; we only
	// assert it produces JSON we can recognize when asked.
	out, _ = runCLI(t, nil, "doctor", "--json")
	if !strings.Contains(out, "\"ready\"") {
		t.Fatalf("doctor --json missing readiness field: %s", out)
	}
}

// TestNaiveInboundCaddyJSON creates a naiveproxy inbound, stages an apply, and
// verifies the generated Caddy JSON config contains the forward_proxy handler
// and the inbound's domain. It then deletes the inbound, re-applies, and
// asserts the generated config no longer references the deleted inbound.
func TestNaiveInboundCaddyJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}
	srv := startServer(t, serverOptions{token: "e2e-secret-token"})

	resp := srv.do(http.MethodPut, "/api/settings", `{"panelListen":"127.0.0.1:2096","mode":"dev","panelAccess":"direct","domain":"vpn.example.com","email":"admin@example.com","naiveUsername":"sysadmin","naivePassword":"syspassword","defaultInboundPublicPort":443}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("settings expected 200, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)

	resp = srv.do(http.MethodPost, "/api/inbounds", `{"name":"naive-tcp","protocol":"naiveproxy","transport":"tcp","port":8443,"enabled":true,"naiveUsername":"alice","naivePassword":"alice-pass","protocolFields":{"domain":"proxy.example.com","transport":"tcp","publicPort":8443}}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("inbound expected 201, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)

	resp = srv.do(http.MethodPost, "/api/apply/plan", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("apply plan expected 200, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	planBody := readJSON(t, resp)
	if valid, ok := planBody["valid"].(bool); !ok || !valid {
		t.Fatalf("expected valid plan, got %v", planBody)
	}
	drain(resp)

	resp = srv.do(http.MethodPost, "/api/apply", `{"confirm":true}`)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusConflict {
		t.Fatalf("apply expected 200/409, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)

	caddyJSON := filepath.Join(srv.applyRoot, "generated", "caddy", "config.json")
	data, err := os.ReadFile(caddyJSON)
	if err != nil {
		t.Fatalf("expected Caddy JSON at %s: %v", caddyJSON, err)
	}
	s := string(data)
	if !strings.Contains(s, "forward_proxy") {
		t.Error("Caddy JSON missing forward_proxy handler")
	}
	if !strings.Contains(s, "proxy.example.com") {
		t.Error("Caddy JSON missing naive domain")
	}

	// Delete the naive inbound and re-apply so the config is regenerated
	// without it.
	resp = srv.do(http.MethodDelete, "/api/inbounds/naive-tcp", "")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete inbound expected 204, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)

	resp = srv.do(http.MethodPost, "/api/apply/plan", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("apply plan after delete expected 200, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	planBody = readJSON(t, resp)
	if valid, ok := planBody["valid"].(bool); !ok || !valid {
		t.Fatalf("expected valid plan after delete, got %v", planBody)
	}
	drain(resp)

	resp = srv.do(http.MethodPost, "/api/apply", `{"confirm":true}`)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusConflict {
		t.Fatalf("apply after delete expected 200/409, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)

	data, err = os.ReadFile(caddyJSON)
	if err != nil {
		if !os.IsNotExist(err) {
			t.Fatalf("unexpected error reading regenerated Caddy JSON at %s: %v", caddyJSON, err)
		}
	} else {
		s = string(data)
		if strings.Contains(s, "forward_proxy") {
			t.Error("Caddy JSON still contains forward_proxy handler after delete")
		}
		if strings.Contains(s, "proxy.example.com") {
			t.Error("Caddy JSON still contains naive domain after delete")
		}
	}
}

// TestRejectsDuplicatePortsEndToEnd confirms the safeguard against
// multiple inbounds trying to listen on the same port surfaces as
// an apply/plan error over the HTTP surface.
func TestRejectsDuplicatePortsEndToEnd(t *testing.T) {
	srv := startServer(t, serverOptions{token: "tok"})

	resp := srv.do(http.MethodPut, "/api/settings", `{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"vpn.example.com","email":"admin@example.com","naiveUsername":"sysadmin","naivePassword":"syspassword"}`)
	drain(resp)

	// First NaiveProxy inbound on port 20001
	resp = srv.do(http.MethodPost, "/api/inbounds", `{"name":"naive-1","protocol":"naiveproxy","transport":"tcp","port":20001,"enabled":true,"naiveUsername":"u1","naivePassword":"p1"}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("inbound 1 expected 201, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)

	// Second NaiveProxy inbound on DIFFERENT port 20002
	resp = srv.do(http.MethodPost, "/api/inbounds", `{"name":"naive-2","protocol":"naiveproxy","transport":"tcp","port":20002,"enabled":true,"naiveUsername":"u2","naivePassword":"p2"}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("inbound 2 expected 201, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)

	// This should successfully plan
	planResp := srv.do(http.MethodPost, "/api/apply/plan", "")
	if planResp.StatusCode != http.StatusOK {
		t.Fatalf("apply plan for different ports expected 200, got %d: %v", planResp.StatusCode, readJSON(t, planResp))
	}
	planBody := readJSON(t, planResp)
	if valid, ok := planBody["valid"].(bool); !ok || !valid {
		t.Fatalf("expected valid plan for different ports, got false: %v", planBody)
	}
	drain(planResp)

	// Third inbound on SAME port as naive-2 (20002)
	resp = srv.do(http.MethodPost, "/api/inbounds", `{"name":"dup-port","protocol":"naiveproxy","transport":"tcp","port":20002,"enabled":true,"naiveUsername":"u3","naivePassword":"p3"}`)

	if resp.StatusCode == http.StatusCreated {
		drain(resp)
		planResp = srv.do(http.MethodPost, "/api/apply/plan", "")
		planBody = readJSON(t, planResp)
		if planResp.StatusCode == http.StatusOK {
			if valid, ok := planBody["valid"].(bool); ok && valid {
				t.Fatalf("expected duplicate port to be rejected during plan, but it was valid: %v", planBody)
			}
			// If plan is OK (but invalid), apply must fail with 400
			applyResp := srv.do(http.MethodPost, "/api/apply", `{"confirm":true}`)
			if applyResp.StatusCode == http.StatusOK {
				t.Fatalf("expected duplicate port to be rejected, but apply succeeded: %v", readJSON(t, applyResp))
			}
			drain(applyResp)
		}
		return
	}
	if resp.StatusCode < 400 {
		t.Fatalf("expected duplicate port inbound to be rejected, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)
}
