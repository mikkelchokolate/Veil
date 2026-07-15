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

	if settings.PanelAccess == "caddy" && settings.PanelDomain != "" {
		domain := strings.ToLower(settings.PanelDomain)
		ensureOwner(owners, domain).Panel = true
		addEmail(emails, domain, settings.PanelEmail)
	}

	for _, inb := range inbounds {
		if inb.Protocol == "naiveproxy" {
			domain := strings.ToLower(naiveDomainWithFallback(inb, settings))
			if domain == "" {
				continue
			}
			ensureOwner(owners, domain).NaiveInboundNames = append(ensureOwner(owners, domain).NaiveInboundNames, inb.Name)
			addEmail(emails, domain, naiveEmailWithFallback(inb, settings))
		}
		if inb.Protocol == "hysteria2" {
			domain := strings.ToLower(stringField(inb.ProtocolFields, "domain"))
			if domain == "" {
				continue
			}
			ensureOwner(owners, domain).HysteriaInboundNames = append(ensureOwner(owners, domain).HysteriaInboundNames, inb.Name)
			addEmail(emails, domain, stringField(inb.ProtocolFields, "email"))
		}
	}

	specs := make(map[string]CaddyDomainCertSpec, len(owners))
	for domain, o := range owners {
		resolved, err := resolveEmail(domain, emails[domain], settings)
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

func resolveEmail(domain string, explicit map[string]struct{}, settings model.Settings) (string, error) {
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
	if d := stringField(inb.ProtocolFields, "domain"); d != "" {
		return d
	}
	return settings.Domain
}

func naiveEmailWithFallback(inb model.Inbound, settings model.Settings) string {
	return stringField(inb.ProtocolFields, "email")
}
