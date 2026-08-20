package routing

import (
	"fmt"
	"net"
	"regexp"
	"strings"
)

// Matcher is one match atom from a routing rule.
type Matcher struct {
	Kind  MatcherKind
	Value string
}

type MatcherKind int

const (
	MatchAll MatcherKind = iota
	MatchPrivateIP
	MatchGeoIP
	MatchGeoSite
	MatchDomain       // exact host
	MatchDomainSuffix // Xray domain: / suffix:
	MatchDomainKeyword
	MatchDomainRegex
	MatchIPCIDR
)

const geoCodeAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789._-!@"

func validGeoCode(code string) bool {
	if code == "" || len(code) > 128 {
		return false
	}
	for _, r := range code {
		if !strings.ContainsRune(geoCodeAlphabet, r) {
			return false
		}
	}
	return true
}

// ParseMatch splits a match field into OR-ed matchers.
// Comma-separated atoms are supported: geosite:category-gov-ru,regexp:.*\.ru$.
func ParseMatch(raw string) ([]Matcher, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("%w: match is empty", ErrRoutingMatchInvalid)
	}
	parts := splitMatchAtoms(raw)
	out := make([]Matcher, 0, len(parts))
	for _, part := range parts {
		matcher, err := parseMatchAtom(part)
		if err != nil {
			return nil, err
		}
		out = append(out, matcher)
	}
	return out, nil
}

func splitMatchAtoms(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func parseMatchAtom(part string) (Matcher, error) {
	lower := strings.ToLower(part)
	switch {
	case lower == "all" || lower == "geosite:all" || lower == "geoip:all":
		return Matcher{Kind: MatchAll}, nil
	case lower == "geoip:private" || lower == "geosite:private":
		return Matcher{Kind: MatchPrivateIP}, nil
	}
	kind, value, ok := splitPrefix(part)
	if !ok {
		if _, ipNet, err := net.ParseCIDR(part); err == nil {
			return Matcher{Kind: MatchIPCIDR, Value: ipNet.String()}, nil
		}
		if ip := net.ParseIP(part); ip != nil {
			if ip.To4() != nil {
				return Matcher{Kind: MatchIPCIDR, Value: ip.String() + "/32"}, nil
			}
			return Matcher{Kind: MatchIPCIDR, Value: ip.String() + "/128"}, nil
		}
		if !validDomainToken(part) {
			return Matcher{}, fmt.Errorf("%w: %q", ErrRoutingMatchInvalid, part)
		}
		return Matcher{Kind: MatchDomainSuffix, Value: strings.TrimPrefix(part, ".")}, nil
	}
	switch kind {
	case "geosite":
		if !validGeoCode(value) {
			return Matcher{}, fmt.Errorf("%w: geosite code %q", ErrRoutingMatchInvalid, value)
		}
		return Matcher{Kind: MatchGeoSite, Value: value}, nil
	case "geoip":
		if strings.EqualFold(value, "private") {
			return Matcher{Kind: MatchPrivateIP}, nil
		}
		if !validGeoCode(value) {
			return Matcher{}, fmt.Errorf("%w: geoip code %q", ErrRoutingMatchInvalid, value)
		}
		return Matcher{Kind: MatchGeoIP, Value: value}, nil
	case "regexp", "regex":
		if value == "" {
			return Matcher{}, fmt.Errorf("%w: empty regexp", ErrRoutingMatchInvalid)
		}
		if _, err := regexp.Compile(value); err != nil {
			return Matcher{}, fmt.Errorf("%w: regexp %q", ErrRoutingMatchInvalid, value)
		}
		return Matcher{Kind: MatchDomainRegex, Value: value}, nil
	case "full":
		if !validDomainToken(value) {
			return Matcher{}, fmt.Errorf("%w: full domain %q", ErrRoutingMatchInvalid, value)
		}
		return Matcher{Kind: MatchDomain, Value: value}, nil
	case "domain", "suffix":
		value = strings.TrimPrefix(value, ".")
		if !validDomainToken(value) {
			return Matcher{}, fmt.Errorf("%w: domain %q", ErrRoutingMatchInvalid, value)
		}
		return Matcher{Kind: MatchDomainSuffix, Value: value}, nil
	case "keyword":
		if value == "" {
			return Matcher{}, fmt.Errorf("%w: empty keyword", ErrRoutingMatchInvalid)
		}
		return Matcher{Kind: MatchDomainKeyword, Value: value}, nil
	case "cidr", "ip":
		if _, ipNet, err := net.ParseCIDR(value); err == nil {
			return Matcher{Kind: MatchIPCIDR, Value: ipNet.String()}, nil
		}
		if ip := net.ParseIP(value); ip != nil {
			if ip.To4() != nil {
				return Matcher{Kind: MatchIPCIDR, Value: ip.String() + "/32"}, nil
			}
			return Matcher{Kind: MatchIPCIDR, Value: ip.String() + "/128"}, nil
		}
		return Matcher{}, fmt.Errorf("%w: cidr %q", ErrRoutingMatchInvalid, value)
	default:
		return Matcher{}, fmt.Errorf("%w: unknown matcher %q", ErrRoutingMatchInvalid, part)
	}
}

func splitPrefix(part string) (kind, value string, ok bool) {
	idx := strings.IndexByte(part, ':')
	if idx <= 0 {
		return "", "", false
	}
	kind = strings.ToLower(part[:idx])
	value = strings.TrimSpace(part[idx+1:])
	switch kind {
	case "geosite", "geoip", "regexp", "regex", "full", "domain", "suffix", "keyword", "cidr", "ip":
		return kind, value, true
	default:
		return "", "", false
	}
}

func validDomainToken(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 253 || strings.ContainsAny(value, " \t\n,") {
		return false
	}
	return true
}
