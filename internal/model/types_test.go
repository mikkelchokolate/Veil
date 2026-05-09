package model

import (
	"encoding/json"
	"testing"
)

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
