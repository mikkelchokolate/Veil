package api

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/bindregistry"
	"github.com/mikkelchokolate/Veil/internal/caddyassembly"
	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/livevalidation"
	"github.com/mikkelchokolate/Veil/internal/model"
)

// hostPortAvailable is a seam for tests; production delegates to the live
// host probe used by live validation (#1168).
var hostPortAvailable = func(ctx context.Context, transport string, port int) (bool, error) {
	return livevalidation.HostPortProbe{}.Available(ctx, transport, port)
}

// demoteForeignHeldHTTP01Binds re-checks planned http-01 challenge binds
// against the live host: the owner registry only knows about Veil's own
// planned listeners, so a foreign service on TCP :80 (nginx, another Caddy,
// a container) is invisible until Caddy fails to load. When :80 is busy and
// every domain behind the challenge bind is hysteria2-only, the bind is
// demoted to a warning — the apply proceeds, the inbound serves its fallback
// certificate, and the post-apply cert-sync retry converges once the port is
// freed (#1168). A bind shared with a Panel or Naive domain keeps error
// severity: those listeners NEED :80 and the apply must fail.
//
// When the live Caddy config already owns :80 the planned challenge bind
// simply merges with it, so no probe is needed at all.
func demoteForeignHeldHTTP01Binds(ctx context.Context, plan *caddyassembly.CaddyRenderPlan, owners map[bindregistry.BindKey]bindregistry.BindOwner, liveRoot string) []model.ValidationIssue {
	var busy []bindregistry.BindKey
	for key, owner := range plan.ACMEChallenges {
		if owner.ChallengeMode == "http-01" && key.Port == 80 && key.Network == bindregistry.ListenTCP {
			busy = append(busy, key)
		}
	}
	if len(busy) == 0 || liveCaddyServesHTTPPort(liveRoot) {
		return nil
	}
	available, err := hostPortAvailable(ctx, "tcp", 80)
	if err != nil || available {
		// A probe failure or a free port means the planned bind stands —
		// either Caddy will own :80 or the load will surface the real error.
		return nil
	}
	var issues []model.ValidationIssue
	for _, key := range busy {
		owner := plan.ACMEChallenges[key]
		hysteriaOnly := true
		var hysteriaNames []string
		for _, domain := range owner.Domains {
			spec := plan.Domains[domain]
			if spec.Owners.Panel || len(spec.Owners.NaiveInboundNames) > 0 || len(spec.Owners.HysteriaInboundNames) == 0 {
				hysteriaOnly = false
				break
			}
			hysteriaNames = append(hysteriaNames, spec.Owners.HysteriaInboundNames...)
		}
		domains := append([]string(nil), owner.Domains...)
		sort.Strings(domains)
		if !hysteriaOnly {
			issues = append(issues, model.ValidationIssue{
				Code:     "acme_http01_port_in_use",
				Severity: "error",
				Message: "TCP :80 is required for http-01 but is owned by a non-Caddy service; ACME certificate cannot be issued for " +
					strings.Join(domains, ", "),
				Source:      "caddyassembly",
				Remediation: "Free TCP port 80 or let a Caddy-managed listener own it, then re-apply.",
			})
			continue
		}
		// Drop the challenge bind entirely: leaving it in the rendered config
		// would make Caddy fail to load and turn the whole apply into a
		// rollback for a problem that only blocks ACME issuance. The domain
		// still keeps http-01 as its issuer mode through
		// plan.HTTP01DeferredDomains — certmagic can then solve the moment
		// the foreign holder frees :80, which is what makes the bounded
		// post-apply sync retry able to converge without a re-apply.
		delete(plan.ACMEChallenges, key)
		delete(owners, key)
		if plan.HTTP01DeferredDomains == nil {
			plan.HTTP01DeferredDomains = make(map[string]bool)
		}
		for _, domain := range domains {
			plan.HTTP01DeferredDomains[domain] = true
		}
		sort.Strings(hysteriaNames)
		issues = append(issues, model.ValidationIssue{
			Code:      "acme_http01_port_in_use",
			Severity:  "warning",
			Field:     model.InboundDomainField,
			InboundID: strings.Join(hysteriaNames, ","),
			Message: "TCP :80 is held by a non-Caddy service, so ACME issuance is deferred for hysteria2 domain(s) " +
				strings.Join(domains, ", ") + "; the inbound keeps serving its self-signed fallback certificate",
			Source: "caddyassembly",
			Remediation: "Free TCP port 80 and re-apply to restore the http-01 challenge server; " +
				"the domain keeps http-01 issuance armed meanwhile, so Veil's background cert-sync picks up the certificate as soon as Caddy can issue it.",
		})
	}
	return issues
}

// liveCaddyServesHTTPPort reports whether the currently live Caddy config
// already binds :80 — in that case a new http-01 challenge bind merges into
// the existing listener instead of colliding with it (#1168).
func liveCaddyServesHTTPPort(liveRoot string) bool {
	body, err := os.ReadFile(filepath.Join(liveRoot, filepath.FromSlash(generatedconfig.CaddyJSONConfigSubpath)))
	if err != nil {
		return false
	}
	var cfg struct {
		Apps struct {
			HTTP struct {
				Servers map[string]struct {
					Listen []string `json:"listen"`
				} `json:"servers"`
			} `json:"http"`
		} `json:"apps"`
	}
	if err := json.Unmarshal(body, &cfg); err != nil {
		return false
	}
	for _, server := range cfg.Apps.HTTP.Servers {
		for _, listen := range server.Listen {
			if strings.HasSuffix(listen, ":80") {
				return true
			}
		}
	}
	return false
}
