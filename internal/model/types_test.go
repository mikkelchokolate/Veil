package model

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestApplyPlanResponseIncludesStructuredPreview(t *testing.T) {
	value := ApplyPlanResponse{
		Valid: true,
		Issues: []ValidationIssue{{
			Code:     "port_in_use",
			Severity: "error",
			Field:    "port",
			Message:  "TCP port 443 is already in use",
			Source:   "live-host",
		}},
		Operations: []ApplyOperation{{
			Type:              "promote_file",
			Destination:       "/etc/veil/generated/caddy/Caddyfile",
			InterruptionRisk:  "reload",
			RollbackAvailable: true,
			ValidationSource:  "live-host",
		}},
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		`"issues"`,
		`"operations"`,
		`"interruptionRisk"`,
		`"rollbackAvailable"`,
		`"validationSource"`,
	} {
		if !bytes.Contains(data, []byte(key)) {
			t.Fatalf("missing %s in %s", key, data)
		}
	}
}

func TestManagementStateModelTypesKeepJSONShape(t *testing.T) {
	state := ManagementSnapshot{
		Settings:      Settings{PanelListen: "127.0.0.1:2096", PanelAccess: "caddy", WebBasePath: "/panel/", Mode: "server"},
		Inbounds:      []Inbound{{Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Profiles: []ClientProfile{{Name: "alice", Username: "alice", Password: "secret", Enabled: true}}}},
		Rules:         []RoutingRule{{Name: "default", Match: "all", Outbound: "direct", Enabled: true}},
		RoutingSource: RoutingSource{Files: []RoutingSourceFile{{Name: "geoip.dat", URL: "https://example.com/geoip.dat", SHA256URL: "https://example.com/geoip.dat.sha256sum"}}},
		Warp:          WarpConfig{Enabled: true, Endpoint: "engage.cloudflareclient.com:2408"},
	}
	body, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, want := range []string{"\"settings\"", "\"panelListen\"", "\"inbounds\"", "\"profiles\"", "\"routingRules\"", "\"routingSource\"", "\"warp\""} {
		if !containsStringInBytes(body, want) {
			t.Fatalf("JSON missing %s: %s", want, string(body))
		}
	}
}

func containsStringInBytes(body []byte, want string) bool {
	return len(want) == 0 || json.Valid(body) && stringContains(string(body), want)
}

func stringContains(value, want string) bool {
	for i := 0; i+len(want) <= len(value); i++ {
		if value[i:i+len(want)] == want {
			return true
		}
	}
	return false
}
