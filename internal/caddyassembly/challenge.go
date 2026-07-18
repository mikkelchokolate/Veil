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

	add := func(key bindregistry.BindKey, mode, domain string) {
		owner := result[key]
		owner.ChallengeMode = mode
		owner.Domains = append(owner.Domains, domain)
		result[key] = owner
	}

	for _, spec := range domains {
		// Hysteria2 is UDP-only and has no Caddy TCP listener, so a domain owned
		// exclusively by hysteria2 inbounds cannot use tls-alpn-01 (there is no
		// Caddy TLS listener for that domain to answer the ALPN challenge). Such
		// domains must use http-01 on :80 instead. This lets users point any
		// domain — even one the Panel never managed — at a hysteria2 inbound and
		// still get a real ACME certificate (no insecure/self-signed fallback),
		// provided :80 is free or already served by a compatible Caddy listener.
		mode := challengeMode
		if mode == "tls-alpn-01" && len(spec.Owners.HysteriaInboundNames) > 0 &&
			!spec.Owners.Panel && len(spec.Owners.NaiveInboundNames) == 0 {
			mode = "http-01"
		}
		switch mode {
		case "http-01":
			key := bindregistry.BindKey{Address: "0.0.0.0", Port: 80, Network: bindregistry.ListenTCP}
			if existing, ok := owners[key]; ok && existing.Kind != bindregistry.BindOwnerAcmeChallenge {
				// A compatible Caddy listener (Panel or Naive) can answer HTTP-01 on :80.
				if existing.Kind == bindregistry.BindOwnerPanelCaddy || existing.Kind == bindregistry.BindOwnerNaive {
					continue
				}
				// For a hysteria2-only domain the missing :80 listener is a
				// warning, not a hard error: the inbound still works via its
				// self-signed/panel cert, it just cannot obtain a public ACME
				// cert until :80 is freed. For Panel/Naive domains a busy :80
				// remains a hard error (those are TCP and rely on the cert).
				severity := "error"
				if len(spec.Owners.HysteriaInboundNames) > 0 && !spec.Owners.Panel && len(spec.Owners.NaiveInboundNames) == 0 {
					severity = "warning"
				}
				issues = append(issues, model.ValidationIssue{
					Code:     "acme_http01_port_in_use",
					Severity: severity,
					Message:  "TCP :80 is required for http-01 but is owned by a non-Caddy service; ACME certificate cannot be issued for " + spec.Domain,
					Source:   "caddyassembly",
				})
				continue
			}
			add(key, mode, spec.Domain)
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
			add(key, mode, spec.Domain)
		case "dns-01":
			// no bind
		}
	}
	return result, issues
}
