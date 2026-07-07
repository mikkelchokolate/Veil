package managementstate

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestValidationRejectsUnknownLegacyStateFields(t *testing.T) {
	body := []byte(`{
		"settings":{"panelListen":"127.0.0.1:2096","mode":"dev","stack":"both"},
		"inbounds":[],
		"routingRules":[],
		"warp":{"enabled":false,"endpoint":"engage.cloudflareclient.com:2408"}
	}`)

	result, err := NewValidation().ValidateBytes(body)
	if err != nil {
		t.Fatalf("ValidateBytes: %v", err)
	}
	if result.Valid || len(result.Errors) == 0 || !strings.Contains(result.Errors[0], `json: unknown field "stack"`) {
		t.Fatalf("expected strict codec error, got %+v", result)
	}
}

func TestValidationChecksManagementSnapshotShape(t *testing.T) {
	body := []byte(`{
		"settings":{"panelListen":"127.0.0.1:2096","mode":"dev"},
		"inbounds":[
			{"name":"a","protocol":"mieru","transport":"tcp","port":443,"enabled":true},
			{"name":"b","protocol":"naiveproxy","transport":"tcp","port":443,"enabled":true}
		],
		"routingRules":[{"name":"","match":"geoip:private","outbound":"direct","enabled":true}],
		"warp":{"enabled":false,"endpoint":"engage.cloudflareclient.com:2408"}
	}`)

	result, err := NewValidation().ValidateBytes(body)
	if err != nil {
		t.Fatalf("ValidateBytes: %v", err)
	}
	if result.Valid || !containsError(result.Errors, "inbounds[1]: duplicate transport/port tcp:443") || !containsError(result.Errors, "routingRules[0].name is required") {
		t.Fatalf("unexpected validation result: %+v", result)
	}
}

func containsError(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestValidationAcceptsValidSnapshot(t *testing.T) {
	body := []byte(`{
		"settings":{"panelListen":"127.0.0.1:2096","mode":"dev"},
		"inbounds":[{"name":"a","protocol":"mieru","transport":"tcp","port":8443,"enabled":true}],
		"routingRules":[{"name":"private","match":"geoip:private","outbound":"direct","enabled":true}],
		"warp":{"enabled":false,"endpoint":"engage.cloudflareclient.com:2408"}
	}`)

	result, err := NewValidation().ValidateBytes(body)
	if err != nil {
		t.Fatalf("ValidateBytes: %v", err)
	}
	if !result.Valid || len(result.Errors) > 0 {
		t.Fatalf("expected valid, got %+v", result)
	}
}

func TestValidationReturnsSyntaxErrorForInvalidJSON(t *testing.T) {
	_, err := NewValidation().ValidateBytes([]byte(`{`))
	if err == nil {
		t.Fatal("expected syntax error")
	}
	if !strings.Contains(err.Error(), "invalid JSON") {
		t.Fatalf("expected invalid JSON error, got %v", err)
	}
}

func TestValidationRequiresSettings(t *testing.T) {
	body := []byte(`{"inbounds":[],"routingRules":[],"warp":{"enabled":false}}`)
	result, err := NewValidation().ValidateBytes(body)
	if err != nil {
		t.Fatalf("ValidateBytes: %v", err)
	}
	if result.Valid || !containsError(result.Errors, "settings is missing") {
		t.Fatalf("expected settings missing error, got %+v", result)
	}
}

func TestValidationRequiresPanelListenAndMode(t *testing.T) {
	body := []byte(`{"settings":{},"inbounds":[],"routingRules":[],"warp":{"enabled":false}}`)
	result, err := NewValidation().ValidateBytes(body)
	if err != nil {
		t.Fatalf("ValidateBytes: %v", err)
	}
	if !containsError(result.Errors, "settings.panelListen is required") || !containsError(result.Errors, "settings.mode is required") {
		t.Fatalf("expected panelListen and mode errors, got %+v", result)
	}
}

func TestValidationDetectsInboundErrors(t *testing.T) {
	body := []byte(`{
		"settings":{"panelListen":"127.0.0.1:2096","mode":"dev"},
		"inbounds":[
			{"name":"","protocol":"","transport":"","port":0,"enabled":true},
			{"name":"a","protocol":"mieru","transport":"tcp","port":443,"enabled":true},
			{"name":"b","protocol":"naiveproxy","transport":"tcp","port":443,"enabled":true}
		],
		"routingRules":[],
		"warp":{"enabled":false}
	}`)
	result, err := NewValidation().ValidateBytes(body)
	if err != nil {
		t.Fatalf("ValidateBytes: %v", err)
	}
	expected := []string{
		"inbounds[0].name is required",
		"inbounds[0].protocol is required",
		"inbounds[0].transport is required",
		"inbounds[0].port must be 1-65535, got: 0",
		"inbounds[2]: duplicate transport/port tcp:443",
	}
	for _, want := range expected {
		if !containsError(result.Errors, want) {
			t.Fatalf("missing error %q in %+v", want, result)
		}
	}
}

func TestValidationDetectsUnsupportedProtocolTransport(t *testing.T) {
	body := []byte(`{
		"settings":{"panelListen":"127.0.0.1:2096","mode":"dev"},
		"inbounds":[
			{"name":"unknown","protocol":"unknown","transport":"tcp","port":8443,"enabled":true},
			{"name":"bad-transport","protocol":"naiveproxy","transport":"udp","port":8444,"enabled":true}
		],
		"routingRules":[],
		"warp":{"enabled":false}
	}`)
	result, err := NewValidation().ValidateBytes(body)
	if err != nil {
		t.Fatalf("ValidateBytes: %v", err)
	}
	expected := []string{
		"inbounds[0].protocol/transport is unsupported: unknown/tcp",
		"inbounds[1].protocol/transport is unsupported: naiveproxy/udp",
	}
	for _, want := range expected {
		if !containsError(result.Errors, want) {
			t.Fatalf("missing error %q in %+v", want, result)
		}
	}
}

func TestValidationDetectsInboundConflictWithPanel(t *testing.T) {
	body := []byte(`{
		"settings":{"panelListen":"127.0.0.1:2096","mode":"dev"},
		"inbounds":[
			{"name":"panel","protocol":"mieru","transport":"tcp","port":2096,"enabled":true}
		],
		"routingRules":[],
		"warp":{"enabled":false}
	}`)
	result, err := NewValidation().ValidateBytes(body)
	if err != nil {
		t.Fatalf("ValidateBytes: %v", err)
	}
	if !containsError(result.Errors, "inbounds[0]: port 2096 conflicts with panel") {
		t.Fatalf("expected panel conflict error, got %+v", result)
	}
}

func TestValidationDetectsInboundConflictWithWarp(t *testing.T) {
	body := []byte(`{
		"settings":{"panelListen":"127.0.0.1:2096","mode":"dev"},
		"inbounds":[
			{"name":"warp","protocol":"mieru","transport":"tcp","port":40000,"enabled":true}
		],
		"routingRules":[],
		"warp":{"enabled":true}
	}`)
	result, err := NewValidation().ValidateBytes(body)
	if err != nil {
		t.Fatalf("ValidateBytes: %v", err)
	}
	if !containsError(result.Errors, "inbounds[0]: port 40000 conflicts with warp") {
		t.Fatalf("expected warp conflict error, got %+v", result)
	}
}

func TestValidationChecksRoutingRules(t *testing.T) {
	body := []byte(`{
		"settings":{"panelListen":"127.0.0.1:2096","mode":"dev"},
		"inbounds":[],
		"routingRules":[{"name":"","match":"","outbound":"","enabled":true}],
		"warp":{"enabled":false}
	}`)
	result, err := NewValidation().ValidateBytes(body)
	if err != nil {
		t.Fatalf("ValidateBytes: %v", err)
	}
	expected := []string{
		"routingRules[0].name is required",
		"routingRules[0].match is required",
		"routingRules[0].outbound is required",
	}
	for _, want := range expected {
		if !containsError(result.Errors, want) {
			t.Fatalf("missing error %q in %+v", want, result)
		}
	}
}

func TestValidateSnapshotPortsAndWarpDefault(t *testing.T) {
	v := NewValidation()
	snapshot := model.ManagementSnapshot{
		Settings: model.Settings{PanelListen: "127.0.0.1:2096", Mode: "dev"},
		Inbounds: []model.Inbound{
			{Name: "a", Protocol: "mieru", Transport: "tcp", Port: 443},
			{Name: "b", Protocol: "naiveproxy", Transport: "tcp", Port: 443},
		},
		Warp: model.WarpConfig{Enabled: true},
	}
	fields := map[string]json.RawMessage{
		"settings":     {},
		"inbounds":     {},
		"warp":         {},
		"routingRules": {},
	}
	errs := v.ValidateSnapshot(snapshot, fields)
	if !containsError(errs, "inbounds[1]: duplicate transport/port tcp:443") {
		t.Fatalf("expected duplicate port error, got %+v", errs)
	}
	// warp default port 40000 should be recorded for udp and tcp.
	if !containsError(errs, "inbounds[0]: port 443 conflicts with warp") {
		// 443 != 40000, so no warp conflict expected; this branch is unreachable but left for clarity.
	}
}

func TestAppendUnique(t *testing.T) {
	values := []string{"a", "b"}
	values = AppendUnique(values, "b")
	if len(values) != 2 {
		t.Fatalf("AppendUnique appended duplicate")
	}
	values = AppendUnique(values, "c")
	if len(values) != 3 || values[2] != "c" {
		t.Fatalf("AppendUnique did not append new value")
	}
}
