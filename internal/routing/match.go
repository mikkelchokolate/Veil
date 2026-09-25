package routing

import (
	"fmt"
	"net"
	"regexp"
	"strings"
	"unicode"
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

// splitMatchAtoms splits a match field on atom-separating commas. Inside a
// regexp atom a comma is a literal pattern character ({m,n} quantifiers,
// character classes, alternation lists) — there is no escape mechanism — so
// a comma only ends a regexp atom when the segment after it opens a new
// prefixed atom such as ",geoip:" or ",domain:" (#1071). A bare domain or
// CIDR intended as a separate matcher must be written before the regexp
// atom.
func splitMatchAtoms(raw string) []string {
	var atoms []string
	var atom strings.Builder
	flush := func() {
		if trimmed := strings.TrimSpace(atom.String()); trimmed != "" {
			atoms = append(atoms, trimmed)
		}
		atom.Reset()
	}
	for index, segment := range strings.Split(raw, ",") {
		if index > 0 {
			if isRegexpMatchAtom(atom.String()) && matchAtomKind(segment) == "" {
				atom.WriteByte(',')
			} else {
				flush()
			}
		}
		atom.WriteString(segment)
	}
	flush()
	return atoms
}

// isRegexpMatchAtom reports whether the atom accumulated so far opened with
// a regexp:/regex: prefix.
func isRegexpMatchAtom(atom string) bool {
	kind := matchAtomKind(atom)
	return kind == "regexp" || kind == "regex"
}

// matchAtomKind returns the recognized lowercase `kind:` prefix of a match
// atom, or "" when the atom carries no known prefix.
func matchAtomKind(atom string) string {
	trimmed := strings.TrimSpace(atom)
	idx := strings.IndexByte(trimmed, ':')
	if idx <= 0 {
		return ""
	}
	kind := strings.ToLower(trimmed[:idx])
	switch kind {
	case "geosite", "geoip", "regexp", "regex", "full", "domain", "suffix", "keyword", "cidr", "ip":
		return kind
	default:
		return ""
	}
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
		// A ':' the prefix table did not recognize is a typo'd kind
		// (gip:cn), a host:port, or an ext:-style dat reference — never a
		// resolvable domain. Reject it instead of minting a domain_suffix
		// matcher that can never match anything (#1071). Bare IPv6 literals
		// and CIDRs already returned above, so reaching ':' here is safe.
		if strings.ContainsRune(part, ':') {
			return Matcher{}, fmt.Errorf("%w: unknown matcher %q", ErrRoutingMatchInvalid, part)
		}
		if !validDomainToken(part) {
			return Matcher{}, fmt.Errorf("%w: %q", ErrRoutingMatchInvalid, part)
		}
		return Matcher{Kind: MatchDomainSuffix, Value: strings.TrimPrefix(part, ".")}, nil
	}
	switch kind {
	case "geosite":
		// Normalize to lowercase: the pinned dat artifacts and the SagerNet
		// rule-set URLs index geo codes lowercase, so a canonically
		// uppercase code like geosite:Category-RU or geoip:CN would
		// otherwise be a silently dead matcher (#1070).
		value = strings.ToLower(value)
		if !validGeoCode(value) {
			return Matcher{}, fmt.Errorf("%w: geosite code %q", ErrRoutingMatchInvalid, value)
		}
		return Matcher{Kind: MatchGeoSite, Value: value}, nil
	case "geoip":
		if strings.EqualFold(value, "private") {
			return Matcher{Kind: MatchPrivateIP}, nil
		}
		value = strings.ToLower(value)
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
	kind = matchAtomKind(part)
	if kind == "" {
		return "", "", false
	}
	// matchAtomKind found the ':' on the trimmed atom, so locate it again on
	// the same trimmed view rather than the raw part.
	trimmed := strings.TrimSpace(part)
	return kind, strings.TrimSpace(trimmed[strings.IndexByte(trimmed, ':')+1:]), true
}

// Hysteria2ACLAddress renders a matcher into the address field of a Hysteria2
// `outbound(address)` ACL line. apernet/hysteria compiles only the geoip:,
// geosite: and suffix: prefixes, `all`/`*`, bare CIDRs and IPs, `*`-wildcard
// domains, and exact domains — it has no keyword or regexp matcher. The
// management dialect's keyword:/regexp: atoms therefore report ok=false
// instead of emitting a fake prefix Hysteria would silently miscompile as a
// literal host (or, for `cidr:X`, reject with "invalid CIDR address" and fail
// the whole config load). Values whose characters corrupt the ACL line
// grammar likewise report ok=false (#679).
func Hysteria2ACLAddress(matcher Matcher) (string, bool) {
	// Value-less kinds are decided before the character safety check, which
	// would reject their empty Value.
	switch matcher.Kind {
	case MatchAll:
		return "all", true
	case MatchPrivateIP:
		return "geoip:private", true
	}
	if !hysteria2ACLValueSafe(matcher.Value) {
		return "", false
	}
	switch matcher.Kind {
	case MatchGeoIP:
		return "geoip:" + matcher.Value, true
	case MatchGeoSite:
		return "geosite:" + matcher.Value, true
	case MatchDomainSuffix:
		return "suffix:" + matcher.Value, true
	case MatchDomain, MatchIPCIDR:
		// Bare values: Hysteria compiles a bare address as CIDR, single IP,
		// *-wildcard, or exact domain — the native forms for the management
		// dialect's full: and cidr: atoms.
		return matcher.Value, true
	default:
		// MatchDomainKeyword and MatchDomainRegex have no Hysteria ACL
		// equivalent.
		return "", false
	}
}

// hysteria2ACLValueSafe reports whether a value survives the upstream ACL
// grammar intact: `#` starts a comment in ParseTextRules (truncating the
// line into InvalidSyntax), `,` and `(`/`)` break or re-shape the
// `outbound(address,...)` line pattern, and whitespace/control characters
// make the field unmatchable. None of these can appear in a real domain,
// CIDR, or geo code.
func hysteria2ACLValueSafe(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r == '#' || r == ',' || r == '(' || r == ')' || unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func validDomainToken(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 253 || strings.ContainsAny(value, " \t\n,") {
		return false
	}
	return true
}
