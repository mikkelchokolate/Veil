//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestFullInboundToApplyFlow drives the complete management lifecycle over a
// real socket: configure settings, create two Mieru inbounds, fetch client
// links, then stage an apply and confirm generated config artifacts land on
// disk under the apply root.
func TestFullInboundToApplyFlow(t *testing.T) {
	srv := startServer(t, serverOptions{token: "e2e-secret-token"})
	inboundPort := freePort(t)

	resp := srv.do(http.MethodPut, "/api/settings", `{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"vpn.example.com"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("settings expected 200, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)

	resp = srv.do(http.MethodPost, "/api/inbounds", fmt.Sprintf(`{"name":"mieru-tcp","protocol":"mieru","transport":"tcp","port":%d,"enabled":true,"profiles":[{"name":"alice","password":"alice-pass","enabled":true}]}`, inboundPort))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("inbound 1 expected 201, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)

	resp = srv.do(http.MethodPost, "/api/inbounds", fmt.Sprintf(`{"name":"mieru-udp","protocol":"mieru","transport":"udp","port":%d,"enabled":true,"password":"udp-pass"}`, inboundPort))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("inbound 2 expected 201, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)

	// Client links should aggregate the enabled Mieru transports — assert the
	// mieru link is actually present, not just any non-empty count (#899).
	resp = srv.do(http.MethodGet, "/api/client-links", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("client links expected 200, got %d", resp.StatusCode)
	}
	links := readJSON(t, resp)
	linkItems, _ := links["links"].([]any)
	foundMieru := false
	for _, item := range linkItems {
		m, _ := item.(map[string]any)
		if m["protocol"] == "mieru" {
			foundMieru = true
		}
	}
	if !foundMieru {
		t.Fatalf("client links carry no mieru entry: %v", links)
	}

	// Plan, then apply; expect generated mieru config under the apply root.
	resp = srv.do(http.MethodPost, "/api/apply/plan", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("apply plan expected 200, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)

	// Stage-only apply must report a clean 200 — a 409 means an auto-apply
	// from the earlier mutations is still in flight, so retry briefly instead
	// of accepting either code (#899).
	var applyResp *http.Response
	for attempt := 0; attempt < 10; attempt++ {
		applyResp = srv.do(http.MethodPost, "/api/apply", `{"confirm":true}`)
		if applyResp.StatusCode != http.StatusConflict {
			break
		}
		drain(applyResp)
		time.Sleep(500 * time.Millisecond)
	}
	if applyResp.StatusCode != http.StatusOK {
		t.Fatalf("apply expected 200, got %d: %v", applyResp.StatusCode, readJSON(t, applyResp))
	}
	// A 200 alone is not proof the stage wrote anything — the plan must be
	// valid and writtenFiles must carry the mieru artifact (#899). `applied`
	// stays false here: this request stages configs without promoting live.
	applyBody := readJSON(t, applyResp)
	if plan, _ := applyBody["plan"].(map[string]any); plan["valid"] != true {
		t.Fatalf("stage apply plan not valid: %v", applyBody)
	}
	written, _ := applyBody["writtenFiles"].([]any)
	foundWritten := false
	for _, f := range written {
		if s, _ := f.(string); strings.HasSuffix(s, "mieru/server_config.json") || strings.Contains(s, "mieru") {
			foundWritten = true
		}
	}
	if !foundWritten {
		t.Fatalf("writtenFiles lacks the mieru artifact: %v", written)
	}

	// The generated mieru artifact lands at a fixed path under the apply root —
	// lock the exact artifact and that it parses as the server config, instead
	// of a substring walk that greens any file with "mieru" in the name (#899).
	mieruConfig := filepath.Join(srv.applyRoot, "generated", "mieru", "server_config.json")
	data, err := os.ReadFile(mieruConfig)
	if err != nil {
		t.Fatalf("expected generated mieru config at %s: %v", mieruConfig, err)
	}
	var rendered struct {
		Users []struct {
			Name string `json:"name"`
		} `json:"users"`
		PortBindings []struct {
			Protocol string `json:"protocol"`
		} `json:"portBindings"`
	}
	if err := json.Unmarshal(data, &rendered); err != nil {
		t.Fatalf("mieru server_config.json is not JSON: %v", err)
	}
	// Both inbounds must be represented: the profile user from mieru-tcp, the
	// fallback inbound user from mieru-udp, and both transport bindings.
	userNames := map[string]bool{}
	for _, u := range rendered.Users {
		userNames[u.Name] = true
	}
	if !userNames["alice"] || !userNames["mieru-udp"] {
		t.Fatalf("mieru users = %v, want alice+mieru-udp: %s", userNames, data)
	}
	transports := map[string]bool{}
	for _, pb := range rendered.PortBindings {
		transports[strings.ToUpper(pb.Protocol)] = true
	}
	if !transports["TCP"] || !transports["UDP"] {
		t.Fatalf("mieru portBindings = %v, want TCP+UDP: %s", transports, data)
	}
}

// TestRejectsDuplicateMieruUsernamesEndToEnd confirms the aggregation
// safeguard (duplicate user names across enabled Mieru inbounds) surfaces as
// an apply/plan error over the real HTTP surface rather than silently writing
// a broken config.
func TestRejectsDuplicateMieruUsernamesEndToEnd(t *testing.T) {
	srv := startServer(t, serverOptions{token: "tok"})
	tcpPort := freePort(t)
	udpPort := freePort(t)

	resp := srv.do(http.MethodPut, "/api/settings", `{"panelListen":"127.0.0.1:2096","mode":"dev"}`)
	drain(resp)

	// Two DIFFERENT Mieru inbounds that share the same client username. Mieru
	// aggregates all users into one global list, so this is the collision the
	// validator must catch — distinct inbound names are required so a mere
	// duplicate-name rejection cannot mask the username check.
	resp = srv.do(http.MethodPost, "/api/inbounds", fmt.Sprintf(`{"name":"dup-a","protocol":"mieru","transport":"tcp","port":%d,"enabled":true,"profiles":[{"name":"shared-user","password":"pw1","enabled":true}]}`, tcpPort))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("inbound 1 expected 201, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)
	resp = srv.do(http.MethodPost, "/api/inbounds", fmt.Sprintf(`{"name":"dup-b","protocol":"mieru","transport":"udp","port":%d,"enabled":true,"profiles":[{"name":"shared-user","password":"pw2","enabled":true}]}`, udpPort))
	// The duplicate username may be rejected at creation, or the conflict may
	// surface at plan/apply time. Either is an acceptable safeguard; what
	// matters is that it never silently succeeds end-to-end — and wherever it
	// surfaces, the mieru_duplicate_username issue must be named.
	if resp.StatusCode == http.StatusCreated {
		drain(resp)
		planResp := srv.do(http.MethodPost, "/api/apply/plan", "")
		planBody := readJSON(t, planResp)
		if planResp.StatusCode == http.StatusOK {
			if valid, _ := planBody["valid"].(bool); valid {
				t.Fatalf("plan reported valid despite duplicate mieru username: %v", planBody)
			}
			if !jsonContainsIssue(planBody, "mieru_duplicate_username") {
				t.Fatalf("plan invalid but missing mieru_duplicate_username issue: %v", planBody)
			}
			// Apply must refuse to render the invalid plan.
			applyResp := srv.do(http.MethodPost, "/api/apply", `{"confirm":true}`)
			if applyResp.StatusCode == http.StatusOK {
				t.Fatalf("expected duplicate mieru user names to be rejected, but apply succeeded: %v", readJSON(t, applyResp))
			}
			drain(applyResp)
			return
		}
		if !jsonContainsIssue(planBody, "mieru_duplicate_username") {
			t.Fatalf("plan failed but without mieru_duplicate_username issue: %v", planBody)
		}
		return
	}
	if resp.StatusCode < 400 {
		t.Fatalf("expected duplicate inbound to be rejected, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	body := readJSON(t, resp)
	if !jsonContainsIssue(body, "mieru_duplicate_username") {
		t.Fatalf("create rejected but without mieru_duplicate_username issue: %v", body)
	}
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

// TestRejectsDuplicatePortsEndToEnd confirms the safeguard against
// multiple inbounds trying to listen on the same port surfaces as
// an apply/plan error over the HTTP surface.
func TestRejectsDuplicatePortsEndToEnd(t *testing.T) {
	srv := startServer(t, serverOptions{token: "tok"})
	// Free ports from the kernel — hardcoded ports flake when a CI worker or
	// leftover process already holds them.
	port1 := freePort(t)
	port2 := freePort(t)

	resp := srv.do(http.MethodPut, "/api/settings", `{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"vpn.example.com","defaultAcmeEmail":"admin@example.com","naiveUsername":"sysadmin","naivePassword":"syspassword"}`)
	drain(resp)

	// First NaiveProxy inbound on the first free port.
	resp = srv.do(http.MethodPost, "/api/inbounds", fmt.Sprintf(`{"name":"naive-1","protocol":"naiveproxy","transport":"tcp","port":%d,"enabled":true,"naiveUsername":"u1","naivePassword":"p1"}`, port1))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("inbound 1 expected 201, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)

	// Second NaiveProxy inbound on a DIFFERENT free port.
	resp = srv.do(http.MethodPost, "/api/inbounds", fmt.Sprintf(`{"name":"naive-2","protocol":"naiveproxy","transport":"tcp","port":%d,"enabled":true,"naiveUsername":"u2","naivePassword":"p2"}`, port2))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("inbound 2 expected 201, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)

	// This should successfully plan.
	planResp := srv.do(http.MethodPost, "/api/apply/plan", "")
	if planResp.StatusCode != http.StatusOK {
		t.Fatalf("apply plan for different ports expected 200, got %d: %v", planResp.StatusCode, readJSON(t, planResp))
	}
	planBody := readJSON(t, planResp)
	if valid, ok := planBody["valid"].(bool); !ok || !valid {
		t.Fatalf("expected valid plan for different ports, got false: %v", planBody)
	}
	drain(planResp)

	// Third inbound on the SAME port as naive-2: wherever the conflict
	// surfaces, the duplicate_binding issue must be named — a bare 4xx could
	// be any unrelated validation error.
	resp = srv.do(http.MethodPost, "/api/inbounds", fmt.Sprintf(`{"name":"dup-port","protocol":"naiveproxy","transport":"tcp","port":%d,"enabled":true,"naiveUsername":"u3","naivePassword":"p3"}`, port2))
	if resp.StatusCode == http.StatusCreated {
		drain(resp)
		planResp = srv.do(http.MethodPost, "/api/apply/plan", "")
		planBody = readJSON(t, planResp)
		if planResp.StatusCode == http.StatusOK {
			if valid, ok := planBody["valid"].(bool); ok && valid {
				t.Fatalf("expected duplicate port to be rejected during plan, but it was valid: %v", planBody)
			}
			if !jsonContainsIssue(planBody, "duplicate_binding") {
				t.Fatalf("plan invalid but missing duplicate_binding issue: %v", planBody)
			}
			// If plan is OK (but invalid), apply must fail.
			applyResp := srv.do(http.MethodPost, "/api/apply", `{"confirm":true}`)
			if applyResp.StatusCode == http.StatusOK {
				t.Fatalf("expected duplicate port to be rejected, but apply succeeded: %v", readJSON(t, applyResp))
			}
			drain(applyResp)
			return
		}
		if !jsonContainsIssue(planBody, "duplicate_binding") {
			t.Fatalf("plan failed but without duplicate_binding issue: %v", planBody)
		}
		return
	}
	if resp.StatusCode < 400 {
		t.Fatalf("expected duplicate port inbound to be rejected, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	// Create-time duplicate rejection is a structured conflict error that must
	// name the transport/port collision — a bare 4xx could be any unrelated
	// validation failure.
	body := readJSON(t, resp)
	errObj, _ := body["error"].(map[string]any)
	errMsg, _ := errObj["message"].(string)
	if errObj["code"] != "conflict" || !strings.Contains(errMsg, "port") {
		t.Fatalf("create rejected but not with the port-conflict error: %v", body)
	}
}
