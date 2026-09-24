package managementstate

import (
	"encoding/json"
	"reflect"
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
			{"name":"b","protocol":"naiveproxy","transport":"tcp","port":443,"enabled":true},
			{"name":"bad.name","protocol":"mieru","transport":"tcp","port":8443,"enabled":true}
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
		"inbounds[3].name must contain only letters, digits, underscore, or hyphen",
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

// #359: the WARP SOCKS listener is TCP-only. A UDP inbound sharing the port
// number is not a real bind conflict and must validate cleanly; the TCP
// reservation must still fire.
func TestValidationWarpSocksPortDoesNotReserveUDP(t *testing.T) {
	udpBody := []byte(`{
		"settings":{"panelListen":"127.0.0.1:2096","mode":"dev"},
		"inbounds":[
			{"name":"hy2","protocol":"hysteria2","transport":"udp","port":40000,"enabled":true}
		],
		"routingRules":[],
		"warp":{"enabled":true}
	}`)
	result, err := NewValidation().ValidateBytes(udpBody)
	if err != nil {
		t.Fatalf("ValidateBytes: %v", err)
	}
	if containsError(result.Errors, "conflicts with warp") {
		t.Fatalf("UDP inbound on socksPort must not conflict with TCP-only WARP listener, got %+v", result)
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
	// Exact set, in order: one error per missing field, nothing extra.
	expected := []string{
		"routingRules[0].name is required",
		"routingRules[0].match is required",
		"routingRules[0].outbound is required",
	}
	if !reflect.DeepEqual(result.Errors, expected) {
		t.Fatalf("routing rule errors = %+v, want exactly %+v", result.Errors, expected)
	}
}

// Multiple offending rules must each produce their own indexed errors — the
// validator reports every problem, not just the first rule's (#943).
func TestValidationReportsEveryBadRoutingRule(t *testing.T) {
	body := []byte(`{
		"settings":{"panelListen":"127.0.0.1:2096","mode":"dev"},
		"inbounds":[],
		"routingRules":[
			{"name":"dup","match":"","outbound":"","enabled":true},
			{"name":"dup","match":"geoip:private","outbound":"direct","enabled":true},
			{"name":"","match":"geosite:test","outbound":"proxy","enabled":true}
		],
		"warp":{"enabled":false}
	}`)
	result, err := NewValidation().ValidateBytes(body)
	if err != nil {
		t.Fatalf("ValidateBytes: %v", err)
	}
	expected := []string{
		"routingRules[0].match is required",
		"routingRules[0].outbound is required",
		`routingRules[1]: duplicate name "dup" also used by routingRules[0]`,
		"routingRules[2].name is required",
	}
	if !reflect.DeepEqual(result.Errors, expected) {
		t.Fatalf("multi-rule errors = %+v, want exactly %+v", result.Errors, expected)
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
	// WARP reserves only its own TCP port (default 40000, #359). An inbound on
	// port 443 does not overlap, so no warp conflict may be reported — the
	// companion case where the inbound DOES share the WARP port is asserted by
	// TestValidationDetectsInboundConflictWithWarp.
	if containsError(errs, "conflicts with warp") {
		t.Fatalf("non-overlapping inbound port reported a warp conflict: %+v", errs)
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
