package applyplan

import (
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

// #679: keyword/regexp matchers and grammar-breaking values have no Hysteria2
// ACL form, so the renderer drops those atoms. The apply plan must surface
// that loss as a warning instead of staying green.
func TestBuildWarnsWhenRuleMatchCannotReachHysteria2(t *testing.T) {
	for _, tc := range []struct {
		match     string
		wantAtoms []string // named in the warning
	}{
		{"keyword:google", []string{"keyword:google"}},
		{`regexp:.*\.ru$`, []string{`regexp:.*\.ru$`}},
		{"suffix:example.com,keyword:foo", []string{"keyword:foo"}}, // partially expressible
		{"full:bad#host", []string{"bad#host"}},                     // '#' would corrupt the ACL line
	} {
		plan := Build(Input{
			Warp: model.WarpConfig{Enabled: true},
			Inbounds: []model.Inbound{
				{Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true},
			},
			Capabilities: []ProtocolCapability{{Protocol: "hysteria2"}},
			Rules: []model.RoutingRule{
				{Name: "r1", Match: tc.match, Outbound: "direct", Enabled: true},
			},
		})
		if !plan.Valid {
			t.Fatalf("match %q: warning must not invalidate the plan, errors: %v", tc.match, plan.Errors)
		}
		found := false
		for _, issue := range plan.Issues {
			if issue.Code == "hysteria2_acl_unsupported_match" {
				found = true
				if issue.Severity != "warning" {
					t.Fatalf("match %q: issue severity = %q, want warning", tc.match, issue.Severity)
				}
				if !strings.Contains(issue.Message, "r1") {
					t.Fatalf("match %q: issue should name the rule: %q", tc.match, issue.Message)
				}
				for _, atom := range tc.wantAtoms {
					if !strings.Contains(issue.Message, atom) {
						t.Fatalf("match %q: issue should name dropped atom %q: %q", tc.match, atom, issue.Message)
					}
				}
			}
		}
		if !found {
			t.Fatalf("match %q: expected hysteria2_acl_unsupported_match issue, got %v", tc.match, plan.Issues)
		}
	}
}

// The warning only applies when Hysteria2 actually renders ACL lines: WARP on
// plus an enabled hysteria2 inbound. Sing-box-only deployments keep full
// keyword/regexp semantics and must not be nagged.
func TestBuildSkipsHysteria2WarningWithoutHysteria2OrWarp(t *testing.T) {
	for name, input := range map[string]Input{
		"no hysteria2 inbound": {
			Warp: model.WarpConfig{Enabled: true},
			Inbounds: []model.Inbound{
				{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true},
			},
			Capabilities: []ProtocolCapability{{Protocol: "naiveproxy"}},
			Rules: []model.RoutingRule{
				{Name: "r1", Match: "keyword:google", Outbound: "direct", Enabled: true},
			},
		},
		"warp disabled": {
			Inbounds: []model.Inbound{
				{Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true},
			},
			Capabilities: []ProtocolCapability{{Protocol: "hysteria2"}},
			Rules: []model.RoutingRule{
				{Name: "r1", Match: "keyword:google", Outbound: "direct", Enabled: true},
			},
		},
		"rule disabled": {
			Warp: model.WarpConfig{Enabled: true},
			Inbounds: []model.Inbound{
				{Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true},
			},
			Capabilities: []ProtocolCapability{{Protocol: "hysteria2"}},
			Rules: []model.RoutingRule{
				{Name: "r1", Match: "keyword:google", Outbound: "direct", Enabled: false},
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			plan := Build(input)
			for _, issue := range plan.Issues {
				if issue.Code == "hysteria2_acl_unsupported_match" {
					t.Fatalf("unexpected hysteria2 ACL issue: %v", issue)
				}
			}
		})
	}
}

// Fully expressible rules produce no warning.
func TestBuildNoHysteria2WarningForExpressibleMatches(t *testing.T) {
	plan := Build(Input{
		Warp: model.WarpConfig{Enabled: true},
		Inbounds: []model.Inbound{
			{Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true},
		},
		Capabilities: []ProtocolCapability{{Protocol: "hysteria2"}},
		Rules: []model.RoutingRule{
			{Name: "r1", Match: "suffix:example.com", Outbound: "direct", Enabled: true},
			{Name: "r2", Match: "full:api.example.com", Outbound: "direct", Enabled: true},
			{Name: "r3", Match: "10.0.0.0/8", Outbound: "warp", Enabled: true},
			{Name: "r4", Match: "geoip:private,geosite:category-gov-ru", Outbound: "direct", Enabled: true},
			{Name: "r5", Match: "all", Outbound: "proxy", Enabled: true},
		},
	})
	for _, issue := range plan.Issues {
		if issue.Code == "hysteria2_acl_unsupported_match" {
			t.Fatalf("unexpected hysteria2 ACL issue for expressible rules: %v", issue)
		}
	}
}
