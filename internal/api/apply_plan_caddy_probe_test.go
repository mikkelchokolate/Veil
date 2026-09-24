package api

import (
	"encoding/json"
	"io/fs"
	"reflect"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/bindregistry"
	"github.com/mikkelchokolate/Veil/internal/caddyassembly"
	"github.com/mikkelchokolate/Veil/internal/caddycapabilities"
	"github.com/mikkelchokolate/Veil/internal/renderer"
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
	// Settings here leave AcmeChallengeMode empty: the planner must be a
	// documented no-op (no acme_* issue), while the renderer still defaults
	// the domain's issuer to tls-alpn-01 — the split #844 pins.
	for _, issue := range plan.Issues {
		if strings.HasPrefix(issue.Code, "acme_") {
			t.Fatalf("empty-mode plan carried an ACME issue: %+v", issue)
		}
	}
	for _, errText := range plan.Errors {
		if strings.Contains(errText, "acme") || strings.Contains(errText, "challenge") {
			t.Fatalf("empty-mode plan carried an ACME error: %q", errText)
		}
	}
}

// TestEmptyAcmeChallengeModePlannerNoOpButRendererDefaultsTLSALPN locks the
// empty- AcmeChallengeMode split (#844): the planner plans no challenge binds
// (default: continue), so no :80 http-01 owner appears for a hysteria2-only
// domain — while renderer.challengeForDomain still defaults the domain's
// issuer to tls-alpn-01. The http-01 switch is reachable only through an
// explicit tls-alpn-01 mode.
func TestEmptyAcmeChallengeModePlannerNoOpButRendererDefaultsTLSALPN(t *testing.T) {
	settings := Settings{
		PanelListen:       "127.0.0.1:2096",
		Mode:              "server",
		DefaultAcmeEmail:  "admin@example.com",
		AcmeChallengeMode: "",
	}
	inbounds := []Inbound{{
		Name:           "hy2",
		Protocol:       "hysteria2",
		Transport:      "udp",
		Port:           443,
		Enabled:        true,
		Password:       "secret",
		ProtocolFields: map[string]any{"domain": "hy2.example.com"},
	}}

	plan, _, issues, err := caddyassembly.BuildFinalRenderPlan(settings, inbounds)
	if err != nil {
		t.Fatalf("BuildFinalRenderPlan: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("empty mode produced issues: %+v", issues)
	}
	if len(plan.ACMEChallenges) != 0 {
		t.Fatalf("empty mode must plan no ACME challenge binds, got %+v", plan.ACMEChallenges)
	}
	httpKey := bindregistry.BindKey{Address: "0.0.0.0", Port: 80, Network: bindregistry.ListenTCP}
	if _, ok := plan.ACMEChallenges[httpKey]; ok {
		t.Fatal("empty mode claimed an :80 http-01 bind for a hy2-only domain")
	}

	// The renderer side of the split: with no planned binds and no default
	// mode, challengeForDomain still emits the domain under a tls-alpn-01
	// issuer — planner and renderer disagree by design, so pin both halves.
	rendered, err := renderer.RenderCaddyJSON(plan, caddycapabilities.CaddyCapabilities{})
	if err != nil {
		t.Fatalf("RenderCaddyJSON: %v", err)
	}
	var doc struct {
		Apps struct {
			TLS struct {
				Automation struct {
					Policies []struct {
						Subjects []string `json:"subjects"`
						Issuers  []struct {
							Module     string `json:"module"`
							Challenges map[string]struct {
								Disabled bool `json:"disabled"`
							} `json:"challenges"`
						} `json:"issuers"`
					} `json:"policies"`
				} `json:"automation"`
			} `json:"tls"`
		} `json:"apps"`
	}
	if err := json.Unmarshal(rendered, &doc); err != nil {
		t.Fatalf("rendered caddy config is not JSON: %v", err)
	}
	found := false
	for _, policy := range doc.Apps.TLS.Automation.Policies {
		for _, subject := range policy.Subjects {
			if subject != "hy2.example.com" {
				continue
			}
			found = true
			if len(policy.Issuers) != 1 || policy.Issuers[0].Module != "acme" {
				t.Fatalf("hy2 domain policy issuers = %+v, want one acme issuer", policy.Issuers)
			}
			alpn, ok := policy.Issuers[0].Challenges["tls-alpn"]
			if !ok || alpn.Disabled {
				t.Fatalf("empty mode must default the hy2 domain issuer to tls-alpn-01, got %+v", policy.Issuers[0].Challenges)
			}
		}
	}
	if !found {
		t.Fatal("no automation policy enrolls hy2.example.com")
	}

	// Explicit tls-alpn-01 is the documented switch: the same hy2-only domain
	// (no Caddy TCP listener for ALPN) must claim an :80 http-01 bind.
	settings.AcmeChallengeMode = "tls-alpn-01"
	explicit, explicitOwners, issues, err := caddyassembly.BuildFinalRenderPlan(settings, inbounds)
	if err != nil {
		t.Fatalf("BuildFinalRenderPlan tls-alpn-01: %v", err)
	}
	owner, ok := explicit.ACMEChallenges[httpKey]
	if !ok {
		t.Fatalf("tls-alpn-01 did not switch the hy2-only domain to an :80 http-01 bind: %+v (issues %v)", explicit.ACMEChallenges, issues)
	}
	if owner.ChallengeMode != "http-01" {
		t.Fatalf("hy2-only domain challenge mode = %q, want http-01", owner.ChallengeMode)
	}
	if len(owner.Domains) != 1 || owner.Domains[0] != "hy2.example.com" {
		t.Fatalf("http-01 bind does not name the hy2 domain: %+v", owner)
	}
	// The challenge bind joins the owner map pinned to the consolidated Caddy
	// unit — the stamp systemd/firewall consumers key off (#971).
	challengeOwner := explicitOwners[httpKey]
	if challengeOwner.Kind != bindregistry.BindOwnerAcmeChallenge || challengeOwner.ServiceName != "veil-caddy.service" {
		t.Fatalf("challenge bind owner = %+v, want AcmeChallenge/veil-caddy.service", challengeOwner)
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
