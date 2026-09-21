package api

import (
	"io/fs"
	"reflect"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/caddycapabilities"
)

// stubCaddyProbe swaps the capability-probe seam for the duration of a test.
func stubCaddyProbe(t *testing.T, fn func(string) (caddycapabilities.CaddyCapabilities, error)) {
	t.Helper()
	old := probeCaddyCapabilities
	probeCaddyCapabilities = fn
	t.Cleanup(func() { probeCaddyCapabilities = old })
}

// Issue #637: a Hysteria2 inbound with a per-inbound domain needs Caddy, but
// the plan must still build when no caddy binary exists yet — apply installs
// it afterwards, and the staging renderer already tolerates the missing
// binary for non-naive plans (audit #156).
func TestBuildApplyPlanToleratesMissingCaddyForHysteria2Domain(t *testing.T) {
	stubCaddyProbe(t, func(string) (caddycapabilities.CaddyCapabilities, error) {
		return caddycapabilities.CaddyCapabilities{}, fs.ErrNotExist
	})

	plan := BuildApplyPlan(ApplyPlanInput{
		LiveRoot: "/etc/veil/generated",
		Settings: Settings{PanelListen: "127.0.0.1:2096", Mode: "server", DefaultAcmeEmail: "admin@example.com"},
		Inbounds: []Inbound{{
			Name:           "hy2",
			Protocol:       "hysteria2",
			Transport:      "udp",
			Port:           443,
			Enabled:        true,
			Password:       "secret",
			ProtocolFields: map[string]any{"domain": "hy2.example.com"},
		}},
	})
	if !plan.Valid {
		t.Fatalf("Hy2+domain plan should stay valid without a caddy binary: %+v", plan)
	}
	if !containsString(plan.Configs, "/etc/veil/generated/caddy/config.json") || !containsString(plan.Runtimes, unitCaddy) {
		t.Fatalf("Hy2+domain plan missing caddy config/runtime: %+v", plan)
	}
}

// Issue #637: naiveproxy still fails closed — without a binary the
// forward_proxy module cannot be probed, so a rendered config could silently
// lack the handler naive depends on.
func TestBuildApplyPlanStillRequiresCaddyBinaryForNaiveProxy(t *testing.T) {
	stubCaddyProbe(t, func(string) (caddycapabilities.CaddyCapabilities, error) {
		return caddycapabilities.CaddyCapabilities{}, fs.ErrNotExist
	})

	plan := BuildApplyPlan(ApplyPlanInput{
		Settings: Settings{PanelListen: "127.0.0.1:2096", Mode: "dev", Domain: "vpn.example.com", DefaultAcmeEmail: "admin@example.com", NaiveUsername: "veil", NaivePassword: "secret"},
		Inbounds: []Inbound{{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true, Password: "secret"}},
	})
	if plan.Valid {
		t.Fatalf("naive plan without a caddy binary should stay invalid: %+v", plan)
	}
	if !strings.Contains(strings.Join(plan.Errors, "\n"), "failed to probe Caddy capabilities") {
		t.Fatalf("expected probe failure error, got %+v", plan.Errors)
	}
}

// Issue #637: the packaged install location is probed when PATH has no caddy,
// so a deployed-but-not-on-PATH binary still yields real capabilities.
func TestBuildApplyPlanProbesPackagedCaddyPathWhenPathMisses(t *testing.T) {
	var probed []string
	stubCaddyProbe(t, func(path string) (caddycapabilities.CaddyCapabilities, error) {
		probed = append(probed, path)
		if path == "" {
			return caddycapabilities.CaddyCapabilities{}, fs.ErrNotExist
		}
		return caddycapabilities.CaddyCapabilities{ForwardProxy: true, HTTP3: true}, nil
	})

	plan := BuildApplyPlan(ApplyPlanInput{
		Settings: Settings{PanelListen: "127.0.0.1:2096", Mode: "dev", Domain: "vpn.example.com", DefaultAcmeEmail: "admin@example.com", NaiveUsername: "veil", NaivePassword: "secret"},
		Inbounds: []Inbound{{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true, Password: "secret"}},
	})
	if !plan.Valid {
		t.Fatalf("plan should use capabilities from the packaged binary: %+v", plan)
	}
	if want := []string{"", "/usr/local/bin/caddy"}; !reflect.DeepEqual(probed, want) {
		t.Fatalf("probe order = %v, want %v", probed, want)
	}
}
