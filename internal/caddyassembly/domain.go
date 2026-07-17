package caddyassembly

import (
	"errors"
	"fmt"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/model"
)

type CaddyDomainOwners struct {
	Panel                bool
	NaiveInboundNames    []string
	HysteriaInboundNames []string
}

type CaddyDomainCertSpec struct {
	Domain string
	Email  string
	Owners CaddyDomainOwners
}

func ResolveDomainCertSpecs(settings model.Settings, inbounds []model.Inbound) (map[string]CaddyDomainCertSpec, error) {
	owners := make(map[string]*CaddyDomainOwners)
	emails := make(map[string]map[string]struct{})

	if settings.PanelAccess == "caddy" {
		panelDomain := strings.ToLower(strings.TrimSpace(settings.PanelDomain))
		if panelDomain == "" {
			panelDomain = strings.ToLower(strings.TrimSpace(settings.Domain))
		}
		if panelDomain != "" {
			ensureOwner(owners, panelDomain).Panel = true
			addEmail(emails, panelDomain, settings.PanelEmail)
		}
	}

	for _, inb := range inbounds {
		if inb.Protocol == "naiveproxy" {
			domain := naiveDomainWithFallback(inb, settings)
			if domain == "" {
				continue
			}
			ensureOwner(owners, domain).NaiveInboundNames = append(ensureOwner(owners, domain).NaiveInboundNames, inb.Name)
			addEmail(emails, domain, naiveInboundEmail(inb))
		}
		if inb.Protocol == "hysteria2" {
			domain := model.InboundDomain(inb)
			if domain == "" {
				continue
			}
			ensureOwner(owners, domain).HysteriaInboundNames = append(ensureOwner(owners, domain).HysteriaInboundNames, inb.Name)
			addEmail(emails, domain, model.InboundEmail(inb))
		}
	}

	specs := make(map[string]CaddyDomainCertSpec, len(owners))
	for domain, o := range owners {
		resolved, err := resolveEmail(domain, emails[domain], settings, o)
		if err != nil {
			return nil, err
		}
		specs[domain] = CaddyDomainCertSpec{Domain: domain, Email: resolved, Owners: *o}
	}
	return specs, nil
}

func ensureOwner(m map[string]*CaddyDomainOwners, domain string) *CaddyDomainOwners {
	if m[domain] == nil {
		m[domain] = &CaddyDomainOwners{}
	}
	return m[domain]
}

func addEmail(m map[string]map[string]struct{}, domain, email string) {
	email = strings.TrimSpace(email)
	if email == "" {
		return
	}
	if m[domain] == nil {
		m[domain] = make(map[string]struct{})
	}
	m[domain][email] = struct{}{}
}

func resolveEmail(domain string, explicit map[string]struct{}, settings model.Settings, owner *CaddyDomainOwners) (string, error) {
	if len(explicit) > 1 {
		return "", fmt.Errorf("domain %s has conflicting ACME emails", domain)
	}
	for e := range explicit {
		return e, nil
	}
	if settings.DefaultAcmeEmail != "" {
		return settings.DefaultAcmeEmail, nil
	}
	if settings.PanelEmail != "" {
		return settings.PanelEmail, nil
	}
	// The legacy global settings.Email is only used as a last-resort fallback
	// for the Panel's own domain. Inbound-specific domains must provide an
	// explicit or default ACME email so a single stale email does not silently
	// issue certificates for unrelated inbound domains.
	if owner != nil && owner.Panel && settings.Email != "" {
		return settings.Email, nil
	}
	return "", errors.New("no ACME email resolved for domain " + domain)
}

func stringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key].(string)
	if !ok {
		return ""
	}
	return v
}

func naiveDomainWithFallback(inb model.Inbound, settings model.Settings) string {
	return model.ResolveInboundDomain(inb, settings)
}

func naiveInboundEmail(inb model.Inbound) string {
	return model.InboundEmail(inb)
}
