package hysteria2

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/hostenv"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/renderer"
)

// ValidateSettings is a no-op for Hysteria2 global settings.
func (Plugin) ValidateSettings(model.Settings, model.Inbound) error { return nil }

// ValidateInbound checks the Hysteria2 inbound for a valid public domain and
// ACME email when a per-inbound domain is configured.
func (Plugin) ValidateInbound(settings model.Settings, inbound model.Inbound) []model.ValidationIssue {
	var issues []model.ValidationIssue
	domain := model.ResolveInboundDomain(inbound, settings)
	if domain == "" {
		issues = append(issues, model.ValidationIssue{
			Code:     "hysteria2_domain_required",
			Severity: "error",
			Field:    model.InboundDomainField,
			Message:  "Hysteria2 inbound requires a public domain.",
			Source:   "hysteria2",
		})
	} else if err := hostenv.ValidateDomain(domain); err != nil {
		issues = append(issues, model.ValidationIssue{
			Code:     "hysteria2_domain_invalid",
			Severity: "error",
			Field:    model.InboundDomainField,
			Message:  "Hysteria2 inbound domain is invalid: " + err.Error(),
			Source:   "hysteria2",
		})
	}
	if model.InboundDomain(inbound) != "" {
		if email := model.ResolveInboundEmail(inbound, settings); email == "" {
			issues = append(issues, model.ValidationIssue{
				Code:     "hysteria2_email_required",
				Severity: "error",
				Field:    model.InboundEmailField,
				Message:  "Hysteria2 inbound with a custom domain requires an ACME email.",
				Source:   "hysteria2",
			})
		} else if err := hostenv.ValidateEmail(email); err != nil {
			issues = append(issues, model.ValidationIssue{
				Code:     "hysteria2_email_invalid",
				Severity: "error",
				Field:    model.InboundEmailField,
				Message:  "Hysteria2 inbound email is invalid: " + err.Error(),
				Source:   "hysteria2",
			})
		}
	}
	// The masquerade URL is rendered verbatim into the server config and
	// upstream refuses to start on an unparsable or non-http(s) URL
	// (audit #64). Validate it here so the panel rejects bad values before
	// apply instead of crash-looping the service.
	if masquerade := masqueradeURL(settings, inbound); masquerade != "" {
		if err := validateMasqueradeURL(masquerade); err != nil {
			issues = append(issues, model.ValidationIssue{
				Code:        "hysteria2_masquerade_invalid",
				Severity:    "error",
				Field:       "masqueradeURL",
				Message:     "Hysteria2 masquerade URL is invalid: " + err.Error(),
				Remediation: "Use an absolute http:// or https:// URL that points at a public website.",
				Source:      "hysteria2",
			})
		}
	}
	return issues
}

// validateMasqueradeURL requires an absolute http(s) URL with a host, mirroring
// what the hysteria2 server accepts at startup, and a public destination.
func validateMasqueradeURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("scheme must be http or https")
	}
	if parsed.Host == "" {
		return errors.New("host is required")
	}
	return validateMasqueradeHost(parsed.Hostname())
}

// masqueradeDenyPrefixes is the canonical egress deny set (the same list the
// veil-hysteria2@.service IPAddressDeny= filter and the Hysteria2 ACL render),
// parsed once.
var masqueradeDenyPrefixes = func() []netip.Prefix {
	cidrs := renderer.EgressDenyCIDRs()
	prefixes := make([]netip.Prefix, 0, len(cidrs))
	for _, cidr := range cidrs {
		prefixes = append(prefixes, netip.MustParsePrefix(cidr))
	}
	return prefixes
}()

// validateMasqueradeHost rejects masquerade targets outside public address
// space (#1292 F5). masquerade.proxy.url is a server-side reverse-proxy
// target: hysteria forwards every unauthenticated HTTP/3 visitor to it, so a
// loopback, private, link-local (cloud metadata), CGNAT, multicast, reserved
// or unspecified target would bridge the internet to a host-local service
// such as Caddy's admin API on 127.42.0.1:2019. The unit's kernel egress
// filter already refuses the dial; mirroring the same deny set here keeps a
// forbidden target from being persisted at all and surfaces the mistake at
// validation time instead of as an opaque runtime 502.
//
// Hostnames that are not IP literals cannot be judged without resolving them
// (and resolution here would be rebindable anyway); those stay covered by the
// kernel filter. Only the names that always mean loopback are rejected
// statically.
func validateMasqueradeHost(host string) error {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "" {
		return errors.New("host is required")
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return fmt.Errorf("host %q is a loopback name; the masquerade target must be a public destination", host)
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		if looksNumericHost(host) {
			// inet_aton shorthand (127.1, 2130706433, 0x7f000001,
			// 0177.0.0.1) is not a canonical literal, but some resolvers
			// still interpret it as an address. A real DNS name never ends
			// in a numeric label, so refuse rather than guess.
			return fmt.Errorf("host %q is a non-canonical numeric address; the masquerade target must be a public destination written as a hostname or canonical IP", host)
		}
		return nil
	}
	addr = addr.WithZone("")
	for _, candidate := range []netip.Addr{addr, addr.Unmap()} {
		for _, prefix := range masqueradeDenyPrefixes {
			if prefix.Contains(candidate) {
				return fmt.Errorf("host %s is in restricted range %s; the masquerade target must be a public destination", host, prefix)
			}
		}
	}
	return nil
}

// looksNumericHost reports whether the last DNS label of host is all decimal
// digits or a 0x-prefixed hex number, i.e. the host is an IPv4 shorthand form
// rather than a domain name.
func looksNumericHost(host string) bool {
	label := host[strings.LastIndex(host, ".")+1:]
	if label == "" {
		return false
	}
	digits := label
	isHex := false
	if strings.HasPrefix(label, "0x") {
		digits = label[2:]
		isHex = true
	}
	for _, r := range digits {
		switch {
		case r >= '0' && r <= '9':
		case isHex && r >= 'a' && r <= 'f':
		default:
			return false
		}
	}
	return true
}

// NeedsDomain reports that Hysteria2 needs a public domain.
func (Plugin) NeedsDomain(model.Settings, model.Inbound) bool { return true }

// NeedsEmail reports that Hysteria2 needs an email address when it uses a
// per-inbound domain with Caddy-managed TLS.
func (Plugin) NeedsEmail(_ model.Settings, inbound model.Inbound) bool {
	return model.InboundDomain(inbound) != ""
}

// HasCredential reports whether the inbound has a usable Hysteria2 credential.
func (Plugin) HasCredential(settings model.Settings, inbound model.Inbound) bool {
	for _, profile := range inbound.Profiles {
		if profile.Enabled && strings.TrimSpace(profile.Password) != "" {
			return true
		}
	}
	password := hysteria2Password(settings, inbound)
	return password != ""
}
