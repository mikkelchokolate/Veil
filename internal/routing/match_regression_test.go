package routing

import (
	"errors"
	"testing"
)

// TestParseMatchKeepsCommasInsideRegexpAtoms covers #1071: a comma inside a
// regexp atom ({m,n} quantifier, alternation) is a pattern character, not an
// atom separator — the naive Split(",") used to corrupt it into a broken
// regex fragment plus a spurious domain matcher.
func TestParseMatchKeepsCommasInsideRegexpAtoms(t *testing.T) {
	cases := []struct {
		raw  string
		want []Matcher
	}{
		{
			`regexp:foo{1,3}`,
			[]Matcher{{Kind: MatchDomainRegex, Value: `foo{1,3}`}},
		},
		{
			`regexp:(a|b)x{2,4}`,
			[]Matcher{{Kind: MatchDomainRegex, Value: `(a|b)x{2,4}`}},
		},
		{
			`regexp:a,b`,
			[]Matcher{{Kind: MatchDomainRegex, Value: `a,b`}},
		},
		// A comma DOES still separate when the next segment opens a new
		// prefixed atom.
		{
			`regexp:.*\.ru$,geoip:cn`,
			[]Matcher{
				{Kind: MatchDomainRegex, Value: `.*\.ru$`},
				{Kind: MatchGeoIP, Value: "cn"},
			},
		},
		{
			`regexp:a{1,2},domain:example.com`,
			[]Matcher{
				{Kind: MatchDomainRegex, Value: `a{1,2}`},
				{Kind: MatchDomainSuffix, Value: "example.com"},
			},
		},
	}
	for _, tc := range cases {
		got, err := ParseMatch(tc.raw)
		if err != nil {
			t.Fatalf("%s: %v", tc.raw, err)
		}
		if len(got) != len(tc.want) {
			t.Fatalf("%s: got %d matchers %+v, want %d", tc.raw, len(got), got, len(tc.want))
		}
		for i := range tc.want {
			if got[i] != tc.want[i] {
				t.Fatalf("%s: matcher %d = %+v, want %+v", tc.raw, i, got[i], tc.want[i])
			}
		}
	}
}

// TestParseMatchRejectsUnknownColonPrefixes covers the second half of #1071:
// an unrecognized kind: prefix or a host:port token used to fall through to
// the bare-domain branch and become a domain_suffix matcher that can never
// match — now it is rejected loudly. Bare IPv6 literals and CIDRs contain
// ':' legitimately and must keep working.
func TestParseMatchRejectsUnknownColonPrefixes(t *testing.T) {
	for _, raw := range []string{"gip:cn", "geositee:x", "example.com:443", "ext:geoip.dat:cn", "host:443"} {
		if _, err := ParseMatch(raw); !errors.Is(err, ErrRoutingMatchInvalid) {
			t.Fatalf("%s: expected ErrRoutingMatchInvalid, got %v", raw, err)
		}
	}
	// IPv6 literals and CIDRs are the ':'-bearing atoms that stay legal.
	for _, raw := range []string{"2001:db8::1", "::1", "2001:db8::/32", "::/0"} {
		matchers, err := ParseMatch(raw)
		if err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		if len(matchers) != 1 || matchers[0].Kind != MatchIPCIDR {
			t.Fatalf("%s: %+v", raw, matchers)
		}
	}
}

// TestParseMatchNormalizesGeoCodesToLowercase covers #1070: the pinned
// geoip.dat and the SagerNet rule-set URLs index geo codes lowercase, so an
// uppercase ISO country code was previously a silently dead matcher on both
// render paths.
func TestParseMatchNormalizesGeoCodesToLowercase(t *testing.T) {
	matchers, err := ParseMatch("geoip:CN")
	if err != nil {
		t.Fatal(err)
	}
	if len(matchers) != 1 || matchers[0] != (Matcher{Kind: MatchGeoIP, Value: "cn"}) {
		t.Fatalf("geoip:CN parsed as %+v, want MatchGeoIP cn", matchers)
	}
	matchers, err = ParseMatch("geosite:Category-RU")
	if err != nil {
		t.Fatal(err)
	}
	if len(matchers) != 1 || matchers[0] != (Matcher{Kind: MatchGeoSite, Value: "category-ru"}) {
		t.Fatalf("geosite:Category-RU parsed as %+v, want MatchGeoSite category-ru", matchers)
	}
	// The Hysteria2 ACL emission must also carry the lowercase code.
	address, ok := Hysteria2ACLAddress(matchers[0])
	if !ok || address != "geosite:category-ru" {
		t.Fatalf("ACL address = %q ok=%v, want geosite:category-ru", address, ok)
	}
}
