package applyplan

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestBuildCollectsInboundValidationErrors(t *testing.T) {
	tests := []struct {
		name      string
		input     Input
		wantErr   []string
		wantValid bool
	}{
		{
			name: "missing name/protocol/transport",
			input: Input{
				Inbounds: []model.Inbound{
					{Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true},
					{Name: "edge", Protocol: "", Transport: "tcp", Port: 443, Enabled: true},
					{Name: "edge", Protocol: "mieru", Transport: "", Port: 443, Enabled: true},
				},
				Capabilities: []ProtocolCapability{{Protocol: "mieru"}},
			},
			wantErr:   []string{"enabled inbounds require name, protocol, and transport"},
			wantValid: false,
		},
		{
			name: "non-positive port",
			input: Input{
				Inbounds: []model.Inbound{
					{Name: "edge", Protocol: "mieru", Transport: "tcp", Port: 0, Enabled: true},
					{Name: "other", Protocol: "mieru", Transport: "tcp", Port: -1, Enabled: true},
				},
				Capabilities: []ProtocolCapability{{Protocol: "mieru"}},
			},
			wantErr:   []string{"enabled inbounds require a positive port"},
			wantValid: false,
		},
		{
			name: "duplicate transport/port",
			input: Input{
				Inbounds: []model.Inbound{
					{Name: "a", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true},
					{Name: "b", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true},
				},
				Capabilities: []ProtocolCapability{{Protocol: "mieru"}},
			},
			wantErr:   []string{"duplicate enabled inbound transport/port"},
			wantValid: false,
		},
		{
			name: "unsupported protocol",
			input: Input{
				Inbounds:     []model.Inbound{{Name: "edge", Protocol: "unknown", Transport: "tcp", Port: 443, Enabled: true}},
				Capabilities: []ProtocolCapability{{Protocol: "mieru"}},
			},
			wantErr:   []string{"unsupported inbound protocol: unknown"},
			wantValid: false,
		},
		{
			name: "unsupported protocol with empty protocol does not add error",
			input: Input{
				Inbounds:     []model.Inbound{{Name: "edge", Protocol: "", Transport: "tcp", Port: 443, Enabled: true}},
				Capabilities: []ProtocolCapability{{Protocol: "mieru"}},
			},
			wantErr:   []string{"enabled inbounds require name, protocol, and transport"},
			wantValid: false,
		},
		{
			name: "capability ValidateSettings error",
			input: Input{
				Inbounds:     []model.Inbound{{Name: "edge", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true}},
				Capabilities: []ProtocolCapability{{Protocol: "mieru", ValidateSettings: func(model.Settings) error { return errors.New("settings invalid") }}},
			},
			wantErr:   []string{"settings invalid"},
			wantValid: false,
		},
		{
			name: "inbound render validation error",
			input: Input{
				Inbounds:                []model.Inbound{{Name: "edge", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true}},
				RenderSettingsAvailable: true,
				Capabilities: []ProtocolCapability{{
					Protocol:               "mieru",
					ValidateInboundRender:  true,
					RequiresRenderSettings: false,
				}},
				ValidateInboundRender: func(model.Inbound) error { return errors.New("render invalid") },
			},
			wantErr:   []string{"render invalid"},
			wantValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := Build(tt.input)
			if plan.Valid != tt.wantValid {
				t.Errorf("Valid = %v, want %v", plan.Valid, tt.wantValid)
			}
			joined := strings.Join(plan.Errors, "\n")
			for _, want := range tt.wantErr {
				if !strings.Contains(joined, want) {
					t.Errorf("errors missing %q: %v", want, plan.Errors)
				}
			}
		})
	}
}

func TestBuildRoutingRulesAndSources(t *testing.T) {
	tests := []struct {
		name      string
		input     Input
		wantErr   []string
		wantValid bool
	}{
		{
			name: "disabled routing rule is skipped",
			input: Input{
				Rules: []model.RoutingRule{{Name: "", Match: "", Outbound: "", Enabled: false}},
			},
			wantErr:   nil,
			wantValid: true,
		},
		{
			name: "enabled routing rule missing fields",
			input: Input{
				Rules: []model.RoutingRule{
					{Name: "", Match: "geosite:test", Outbound: "direct", Enabled: true},
					{Name: "rule", Match: "", Outbound: "direct", Enabled: true},
					{Name: "rule", Match: "geosite:test", Outbound: "", Enabled: true},
				},
			},
			wantErr:   []string{"enabled routing rules require name, match, and outbound"},
			wantValid: false,
		},
		{
			name: "direct and proxy outbounds are accepted",
			input: Input{
				Rules: []model.RoutingRule{
					{Name: "direct-rule", Match: "geosite:test", Outbound: "direct", Enabled: true},
					{Name: "proxy-rule", Match: "geosite:test", Outbound: "proxy", Enabled: true},
				},
			},
			wantErr:   nil,
			wantValid: true,
		},
		{
			name: "warp outbound without warp enabled",
			input: Input{
				Rules: []model.RoutingRule{{Name: "warp-rule", Match: "geosite:test", Outbound: "warp", Enabled: true}},
				Warp:  model.WarpConfig{Enabled: false},
			},
			wantErr:   []string{"routing rule warp-rule requires WARP to be enabled"},
			wantValid: false,
		},
		{
			name: "unsupported routing outbound",
			input: Input{
				Rules: []model.RoutingRule{{Name: "bad-rule", Match: "geosite:test", Outbound: "blackhole", Enabled: true}},
			},
			wantErr:   []string{"unsupported routing outbound: blackhole"},
			wantValid: false,
		},
		{
			name: "routing source file missing fields",
			input: Input{
				RoutingSource: model.RoutingSource{Files: []model.RoutingSourceFile{
					{Name: "", URL: "https://example.com/geoip.dat"},
					{Name: "geoip.dat", URL: ""},
					{Name: "", URL: ""},
				}},
			},
			wantErr:   []string{"routing source files require name and URL"},
			wantValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := Build(tt.input)
			if plan.Valid != tt.wantValid {
				t.Errorf("Valid = %v, want %v", plan.Valid, tt.wantValid)
			}
			joined := strings.Join(plan.Errors, "\n")
			for _, want := range tt.wantErr {
				if !strings.Contains(joined, want) {
					t.Errorf("errors missing %q: %v", want, plan.Errors)
				}
			}
			for _, err := range plan.Errors {
				if tt.wantErr == nil {
					t.Errorf("unexpected error: %q", err)
					continue
				}
			}
		})
	}
}

func TestBuildWarpRenderValidationError(t *testing.T) {
	plan := Build(Input{
		Warp:               model.WarpConfig{Enabled: true},
		ValidateWarpRender: func() error { return errors.New("warp render invalid") },
	})
	if plan.Valid {
		t.Errorf("expected invalid plan")
	}
	if !strings.Contains(strings.Join(plan.Errors, "\n"), "warp render invalid") {
		t.Errorf("expected warp render error, got: %v", plan.Errors)
	}
}

func TestBuildOperationsSkipsUnknownServiceVerbs(t *testing.T) {
	ops := buildOperations(
		[]string{"/etc/veil/generated/x.json"},
		[]string{"validate management state", "stage generated configs", "start veil-x.service"},
		[]string{"veil-x.service"},
		"", "",
	)
	for _, op := range ops {
		if op.Type == "start_service" {
			t.Errorf("unexpected start_service operation: %+v", op)
		}
	}
}

func TestServiceActionVerbsIgnoresInvalidActions(t *testing.T) {
	verbs := serviceActionVerbs([]string{
		"reload veil-a.service",
		"not-a-verb veil-b.service",
		"restart",
		"reload  veil-c.service extra",
	})
	want := map[string]string{
		"veil-a.service": "reload",
	}
	if !reflect.DeepEqual(verbs, want) {
		t.Errorf("got %v, want %v", verbs, want)
	}
}

func TestServiceVerbForUnit(t *testing.T) {
	verbs := map[string]string{
		"veil-mieru.service":  "restart",
		"veil-caddy@.service": "reload",
		"veil-other@.service": "not-a-verb",
	}

	tests := []struct {
		unit string
		want string
	}{
		{"veil-mieru.service", "restart"},
		{"veil-caddy@edge.service", "reload"},
		{"veil-caddy@panel.service", "reload"},
		{"veil-unknown.service", ""},
		{"veil-other@foo.service", "not-a-verb"},
	}

	for _, tt := range tests {
		t.Run(tt.unit, func(t *testing.T) {
			if got := serviceVerbForUnit(tt.unit, verbs); got != tt.want {
				t.Errorf("serviceVerbForUnit(%q) = %q, want %q", tt.unit, got, tt.want)
			}
		})
	}
}

func TestAppendUnique(t *testing.T) {
	tests := []struct {
		values []string
		next   string
		want   []string
	}{
		{[]string{"a", "b"}, "c", []string{"a", "b", "c"}},
		{[]string{"a", "b"}, "b", []string{"a", "b"}},
		{[]string{"a", "b"}, "", []string{"a", "b"}},
	}

	for _, tt := range tests {
		t.Run(tt.next, func(t *testing.T) {
			got := appendUnique(tt.values, tt.next)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("appendUnique(%v, %q) = %v, want %v", tt.values, tt.next, got, tt.want)
			}
		})
	}
}

func TestBuildDoesNotIncludeEmptyCapabilityConfigOrAction(t *testing.T) {
	plan := Build(Input{
		Inbounds:     []model.Inbound{{Name: "edge", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true}},
		Capabilities: []ProtocolCapability{{Protocol: "mieru"}},
	})
	if !plan.Valid {
		t.Fatalf("unexpected errors: %v", plan.Errors)
	}
	for _, cfg := range plan.Configs {
		if cfg == "" {
			t.Errorf("unexpected empty config")
		}
	}
	for _, action := range plan.Actions {
		if action == "" {
			t.Errorf("unexpected empty action")
		}
	}
}
