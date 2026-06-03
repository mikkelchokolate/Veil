//go:build e2e

package e2e

import (
	"net/http"
	"testing"
)

// TestFirewallRulesReflectSettingsAndInbounds verifies that the /api/firewall
// endpoint returns the expected active state and correctly planned rule responses
// matching the configured settings and enabled inbounds.
func TestFirewallRulesReflectSettingsAndInbounds(t *testing.T) {
	srv := startServer(t, serverOptions{token: "e2e-secret-token"})

	// 1. Setup Panel settings
	resp := srv.do(http.MethodPut, "/api/settings", `{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"vpn.example.com"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("settings expected 200, got %d", resp.StatusCode)
	}
	drain(resp)

	// 2. Add an enabled Hysteria2 Inbound on port 8443
	resp = srv.do(http.MethodPost, "/api/inbounds", `{"name":"hy2-inbound","protocol":"hysteria2","transport":"udp","port":8443,"enabled":true,"password":"pass"}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("inbound expected 201, got %d", resp.StatusCode)
	}
	drain(resp)

	// 3. Add a disabled Mieru Inbound on port 9443 (should not appear in firewall rules)
	resp = srv.do(http.MethodPost, "/api/inbounds", `{"name":"mieru-disabled","protocol":"mieru","transport":"tcp","port":9443,"enabled":false,"password":"pass"}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("inbound expected 201, got %d", resp.StatusCode)
	}
	drain(resp)

	// 4. Query firewall rules
	resp = srv.do(http.MethodGet, "/api/firewall", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("firewall rules expected 200, got %d", resp.StatusCode)
	}
	body := readJSON(t, resp)
	drain(resp)

	// Assert the JSON payload
	rules, ok := body["rules"].([]any)
	if !ok {
		t.Fatalf("expected rules slice in response, got: %+v", body)
	}

	foundPanel := false
	foundHy2 := false
	foundMieru := false

	for _, rawRule := range rules {
		rule, ok := rawRule.(map[string]any)
		if !ok {
			continue
		}
		port, _ := rule["port"].(float64)
		proto, _ := rule["protocol"].(string)
		service, _ := rule["service"].(string)

		if int(port) == 2096 && proto == "tcp" && service == "Veil panel" {
			foundPanel = true
		}
		if int(port) == 8443 && proto == "udp" && (service == "Hysteria2" || service == "Veil Hysteria2") {
			foundHy2 = true
		}
		if int(port) == 9443 {
			foundMieru = true
		}
	}

	if !foundPanel {
		t.Errorf("expected to find firewall rule for panel on port 2096/tcp, rules: %+v", rules)
	}
	if !foundHy2 {
		t.Errorf("expected to find firewall rule for enabled Hysteria2 on port 8443/udp, rules: %+v", rules)
	}
	if foundMieru {
		t.Errorf("should NOT find firewall rule for disabled Mieru on port 9443, rules: %+v", rules)
	}
}
