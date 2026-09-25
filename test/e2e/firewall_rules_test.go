//go:build e2e

package e2e

import (
	"fmt"
	"net/http"
	"testing"
)

// TestFirewallRulesReflectSettingsAndInbounds verifies that the /api/firewall
// endpoint returns the expected active state and correctly planned rule responses
// matching the configured settings and enabled inbounds.
//
// The panel rule contract is the audit #356 security boundary: a loopback-only
// panel listener must NOT produce a public "Veil panel" allow rule (it would
// punch a useless external allow and be mistaken for management access when UFW
// is enabled), while a non-loopback listener must produce one.
func TestFirewallRulesReflectSettingsAndInbounds(t *testing.T) {
	// Seed empty routing rules so the mutation-triggered applies never fetch
	// route-dat over the network — this test asserts firewall planning only.
	srv := startServer(t, serverOptions{token: "e2e-secret-token", seedState: seedStateNoRouteDat})
	hysteriaPort := freePort(t)
	disabledPort := freePort(t)
	if disabledPort == hysteriaPort {
		disabledPort = freePort(t)
	}

	// 1. Setup Panel settings with a loopback-only panel listener.
	resp := srv.do(http.MethodPut, "/api/settings", `{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"vpn.example.com"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("settings expected 200, got %d", resp.StatusCode)
	}
	drain(resp)

	// 2. Add an enabled Hysteria2 Inbound on a free port
	resp = srv.do(http.MethodPost, "/api/inbounds", fmt.Sprintf(`{"name":"hy2-inbound","protocol":"hysteria2","transport":"udp","port":%d,"enabled":true,"password":"pass"}`, hysteriaPort))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("inbound expected 201, got %d", resp.StatusCode)
	}
	drain(resp)

	// 3. Add a disabled Mieru Inbound (should not appear in firewall rules)
	resp = srv.do(http.MethodPost, "/api/inbounds", fmt.Sprintf(`{"name":"mieru-disabled","protocol":"mieru","transport":"tcp","port":%d,"enabled":false,"password":"pass"}`, disabledPort))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("inbound expected 201, got %d", resp.StatusCode)
	}
	drain(resp)

	// 4. Query firewall rules
	rules := firewallRules(t, srv)

	foundPanel := false
	foundHy2 := false
	foundMieru := false

	for _, rule := range rules {
		port, _ := rule["port"].(float64)
		proto, _ := rule["protocol"].(string)
		service, _ := rule["service"].(string)

		if int(port) == 2096 && proto == "tcp" && service == "Veil panel" {
			foundPanel = true
		}
		if int(port) == hysteriaPort && proto == "udp" && (service == "Hysteria2" || service == "Veil Hysteria2") {
			foundHy2 = true
		}
		if int(port) == disabledPort {
			foundMieru = true
		}
	}

	if foundPanel {
		t.Errorf("loopback-only panel must NOT produce a public 'Veil panel' allow rule on 2096/tcp (audit #356), rules: %+v", rules)
	}
	if !foundHy2 {
		t.Errorf("expected to find firewall rule for enabled Hysteria2 on port %d/udp, rules: %+v", hysteriaPort, rules)
	}
	if foundMieru {
		t.Errorf("should NOT find firewall rule for disabled Mieru on port %d, rules: %+v", disabledPort, rules)
	}

	// 5. Reconfigure the panel on a non-loopback listen address: the public
	// 'Veil panel' rule must now be planned. The panelListen setting only
	// feeds firewall planning here; the running test process keeps its real
	// VEIL_LISTEN loopback bind.
	resp = srv.do(http.MethodPut, "/api/settings", `{"panelListen":"0.0.0.0:2096","panelAccess":"direct","mode":"dev","domain":"vpn.example.com"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("settings (non-loopback panelListen) expected 200, got %d", resp.StatusCode)
	}
	drain(resp)

	rules = firewallRules(t, srv)
	foundPanel = false
	for _, rule := range rules {
		port, _ := rule["port"].(float64)
		proto, _ := rule["protocol"].(string)
		service, _ := rule["service"].(string)
		if int(port) == 2096 && proto == "tcp" && service == "Veil panel" {
			foundPanel = true
		}
	}
	if !foundPanel {
		t.Errorf("expected 'Veil panel' firewall rule on 2096/tcp for non-loopback panelListen, rules: %+v", rules)
	}
}

// firewallRules fetches GET /api/firewall and returns the rules array.
func firewallRules(t *testing.T, srv *serverProc) []map[string]any {
	t.Helper()
	resp := srv.do(http.MethodGet, "/api/firewall", "")
	if resp.StatusCode != http.StatusOK {
		drain(resp)
		t.Fatalf("firewall rules expected 200, got %d", resp.StatusCode)
	}
	body := readJSON(t, resp)

	rawRules, ok := body["rules"].([]any)
	if !ok {
		t.Fatalf("expected rules slice in response, got: %+v", body)
	}
	rules := make([]map[string]any, 0, len(rawRules))
	for _, raw := range rawRules {
		if rule, ok := raw.(map[string]any); ok {
			rules = append(rules, rule)
		}
	}
	return rules
}
