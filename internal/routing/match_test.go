package routing

import (
	"errors"
	"testing"
)

func TestParseMatch3xUICommaSeparatedDirect(t *testing.T) {
	matchers, err := ParseMatch(`geosite:category-gov-ru,regexp:.*\.ru$,regexp:.*\.su$`)
	if err != nil {
		t.Fatal(err)
	}
	if len(matchers) != 3 {
		t.Fatalf("got %d matchers: %+v", len(matchers), matchers)
	}
	if matchers[0] != (Matcher{Kind: MatchGeoSite, Value: "category-gov-ru"}) {
		t.Fatalf("geosite: %+v", matchers[0])
	}
	if matchers[1] != (Matcher{Kind: MatchDomainRegex, Value: `.*\.ru$`}) {
		t.Fatalf("ru regex: %+v", matchers[1])
	}
	if matchers[2] != (Matcher{Kind: MatchDomainRegex, Value: `.*\.su$`}) {
		t.Fatalf("su regex: %+v", matchers[2])
	}
}

func TestParseMatchKnownAtoms(t *testing.T) {
	cases := []struct {
		raw  string
		kind MatcherKind
		val  string
	}{
		{"geoip:private", MatchPrivateIP, ""},
		{"geoip:ru", MatchGeoIP, "ru"},
		{"geosite:openai", MatchGeoSite, "openai"},
		{"geosite:geolocation-!ru", MatchGeoSite, "geolocation-!ru"},
		{"all", MatchAll, ""},
		{"example.com", MatchDomainSuffix, "example.com"},
		{"domain:example.com", MatchDomainSuffix, "example.com"},
		{"full:api.example.com", MatchDomain, "api.example.com"},
		{"keyword:google", MatchDomainKeyword, "google"},
		{"10.0.0.0/8", MatchIPCIDR, "10.0.0.0/8"},
		{"regex:foo.*", MatchDomainRegex, "foo.*"},
	}
	for _, tc := range cases {
		got, err := ParseMatch(tc.raw)
		if err != nil {
			t.Fatalf("%s: %v", tc.raw, err)
		}
		if len(got) != 1 || got[0].Kind != tc.kind || got[0].Value != tc.val {
			t.Fatalf("%s: %+v", tc.raw, got)
		}
	}
}

func TestParseMatchRejectsBrokenRegexp(t *testing.T) {
	_, err := ParseMatch(`regexp:[`)
	if err == nil || !errors.Is(err, ErrRoutingMatchInvalid) {
		t.Fatalf("expected invalid regexp, got %v", err)
	}
}

func TestParseMatchRejectsInvalidGeoCode(t *testing.T) {
	_, err := ParseMatch("geosite:category-gov-ru,regexp:")
	if err == nil || !errors.Is(err, ErrRoutingMatchInvalid) {
		t.Fatalf("expected invalid empty regexp, got %v", err)
	}
	_, err = ParseMatch("geosite:foo/bar")
	if err == nil || !errors.Is(err, ErrRoutingMatchInvalid) {
		t.Fatalf("expected invalid geosite code, got %v", err)
	}
}
