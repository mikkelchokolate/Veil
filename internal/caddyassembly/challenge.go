package caddyassembly

import (
	"github.com/mikkelchokolate/Veil/internal/bindregistry"
	"github.com/mikkelchokolate/Veil/internal/model"
)

type AcmeChallengeOwner struct {
	ChallengeMode string
	Domains       []string
}

func PlanAcmeChallengeBinds(
	challengeMode string,
	domains map[string]CaddyDomainCertSpec,
	owners map[bindregistry.BindKey]bindregistry.BindOwner,
) (map[bindregistry.BindKey]AcmeChallengeOwner, []model.ValidationIssue) {
	result := make(map[bindregistry.BindKey]AcmeChallengeOwner)
	var issues []model.ValidationIssue

	add := func(key bindregistry.BindKey, domain string) {
		owner := result[key]
		owner.ChallengeMode = challengeMode
		owner.Domains = append(owner.Domains, domain)
		result[key] = owner
	}

	for _, spec := range domains {
		switch challengeMode {
		case "http-01":
			key := bindregistry.BindKey{Address: "0.0.0.0", Port: 80, Network: bindregistry.ListenTCP}
			if existing, ok := owners[key]; ok && existing.Kind != bindregistry.BindOwnerAcmeChallenge {
				// A compatible Caddy listener (Panel or Naive) can answer HTTP-01 on :80.
				if existing.Kind == bindregistry.BindOwnerPanelCaddy || existing.Kind == bindregistry.BindOwnerNaive {
					continue
				}
				issues = append(issues, model.ValidationIssue{
					Code:     "acme_http01_port_in_use",
					Severity: "error",
					Message:  "TCP :80 is required for http-01 but is owned by a non-Caddy service",
					Source:   "caddyassembly",
				})
				continue
			}
			add(key, spec.Domain)
		case "tls-alpn-01":
			key := bindregistry.BindKey{Address: "0.0.0.0", Port: 443, Network: bindregistry.ListenTCP}
			if existing, ok := owners[key]; ok && existing.Kind != bindregistry.BindOwnerAcmeChallenge {
				// TLS-ALPN-01 can reuse a compatible Caddy TCP :443 listener; if owner is not Caddy, reject.
				if existing.Kind != bindregistry.BindOwnerPanelCaddy && existing.Kind != bindregistry.BindOwnerNaive {
					issues = append(issues, model.ValidationIssue{
						Code:     "acme_tlsalpn_port_in_use",
						Severity: "error",
						Message:  "TCP :443 is required for tls-alpn-01 but is owned by a non-Caddy service",
						Source:   "caddyassembly",
					})
					continue
				}
				// A compatible Caddy owner already answers TLS-ALPN-01 on :443; do not
				// add a challenge-only bind that would replace it.
				continue
			}
			add(key, spec.Domain)
		case "dns-01":
			// no bind
		}
	}
	return result, issues
}
