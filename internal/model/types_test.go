package model

import (
	"encoding/json"
	"reflect"
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
	var decoded ApplyPlanResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("plan JSON does not round-trip: %v", err)
	}
	if !reflect.DeepEqual(decoded, value) {
		t.Fatalf("plan JSON round-trip mismatch:\n got %+v\nwant %+v", decoded, value)
	}
	// Lock the wire field names explicitly so a tag rename is caught even if
	// the Go struct round-trips.
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	issues, ok := raw["issues"].([]any)
	if !ok || len(issues) != 1 {
		t.Fatalf("issues field missing or wrong shape: %s", data)
	}
	issue := issues[0].(map[string]any)
	if issue["code"] != "port_in_use" || issue["severity"] != "error" || issue["source"] != "live-host" {
		t.Fatalf("issue fields = %v", issue)
	}
	operations, ok := raw["operations"].([]any)
	if !ok || len(operations) != 1 {
		t.Fatalf("operations field missing or wrong shape: %s", data)
	}
	operation := operations[0].(map[string]any)
	if operation["type"] != "promote_file" || operation["interruptionRisk"] != "reload" ||
		operation["rollbackAvailable"] != true || operation["validationSource"] != "live-host" {
		t.Fatalf("operation fields = %v", operation)
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
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("snapshot JSON is not an object: %v", err)
	}
	settings, ok := raw["settings"].(map[string]any)
	if !ok {
		t.Fatalf("missing settings object: %s", body)
	}
	if settings["panelListen"] != "127.0.0.1:2096" || settings["panelAccess"] != "caddy" ||
		settings["webBasePath"] != "/panel/" || settings["mode"] != "server" {
		t.Fatalf("settings values = %v", settings)
	}
	inbounds, ok := raw["inbounds"].([]any)
	if !ok || len(inbounds) != 1 {
		t.Fatalf("missing inbounds array: %s", body)
	}
	inbound := inbounds[0].(map[string]any)
	if inbound["name"] != "mieru" || inbound["protocol"] != "mieru" || inbound["port"] != float64(443) || inbound["enabled"] != true {
		t.Fatalf("inbound values = %v", inbound)
	}
	profiles, ok := inbound["profiles"].([]any)
	if !ok || len(profiles) != 1 {
		t.Fatalf("missing profiles array: %s", body)
	}
	profile := profiles[0].(map[string]any)
	if profile["username"] != "alice" || profile["password"] != "secret" || profile["enabled"] != true {
		t.Fatalf("profile values = %v", profile)
	}
	rules, ok := raw["routingRules"].([]any)
	if !ok || len(rules) != 1 {
		t.Fatalf("missing routingRules array: %s", body)
	}
	rule := rules[0].(map[string]any)
	if rule["name"] != "default" || rule["outbound"] != "direct" || rule["enabled"] != true {
		t.Fatalf("routing rule values = %v", rule)
	}
	source, ok := raw["routingSource"].(map[string]any)
	if !ok {
		t.Fatalf("missing routingSource object: %s", body)
	}
	files, ok := source["files"].([]any)
	if !ok || len(files) != 1 {
		t.Fatalf("missing routingSource.files array: %s", body)
	}
	file := files[0].(map[string]any)
	if file["name"] != "geoip.dat" || file["url"] != "https://example.com/geoip.dat" ||
		file["sha256Url"] != "https://example.com/geoip.dat.sha256sum" {
		t.Fatalf("routing source file values = %v", file)
	}
	warp, ok := raw["warp"].(map[string]any)
	if !ok {
		t.Fatalf("missing warp object: %s", body)
	}
	if warp["enabled"] != true || warp["endpoint"] != "engage.cloudflareclient.com:2408" {
		t.Fatalf("warp values = %v", warp)
	}
}
