package renderer

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// This file gates rendered acl.inline lines against a faithful copy of the
// apernet/hysteria ACL grammar (extras/outbounds/acl/parse.go +
// compile.go's compileHostMatcher). #679 shipped lines like
// direct(cidr:1.2.3.0/24) / direct(regex:...) that this grammar either
// rejects at config load or silently compiles as a literal host match, and
// #680 locked that dialect in via substring tests. The gate below is the
// same parse+compile pipeline the Hysteria binary runs, minus geo database
// loading (geo prefixes are validated for a non-empty code/name only, since
// compile only touches the DB after that check).

var hysteriaACLLinePattern = regexp.MustCompile(`^(\w+)\s*\(([^,]+)(?:,([^,]+))?(?:,([^,]+))?\)$`)

type hysteriaACLTextRule struct {
	outbound string
	address  string
	lineNum  int
}

// hysteriaACLParseTextRules mirrors upstream ParseTextRules: `#` starts a
// comment and is stripped before the line is parsed, blank lines are skipped,
// and anything not matching outbound(address,...) is an InvalidSyntaxError.
func hysteriaACLParseTextRules(text string) ([]hysteriaACLTextRule, error) {
	var rules []hysteriaACLTextRule
	lineNum := 0
	for _, line := range strings.Split(text, "\n") {
		lineNum++
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		m := hysteriaACLLinePattern.FindStringSubmatch(line)
		if m == nil {
			return nil, fmt.Errorf("invalid syntax at line %d: %s", lineNum, line)
		}
		rules = append(rules, hysteriaACLTextRule{
			outbound: m[1],
			address:  strings.TrimSpace(m[2]),
			lineNum:  lineNum,
		})
	}
	return rules, nil
}

// hysteriaACLCompileAddress mirrors upstream compileHostMatcher dispatch and
// returns the matcher kind the address compiles to. geoip:/geosite: stop at
// the non-empty check — the DB lookup upstream would follow is not part of
// the line grammar.
func hysteriaACLCompileAddress(addr string) (string, error) {
	addr = strings.TrimRight(strings.ToLower(addr), ".")
	if addr == "*" || addr == "all" {
		return "all", nil
	}
	if strings.HasPrefix(addr, "geoip:") {
		if addr[len("geoip:"):] == "" {
			return "", fmt.Errorf("empty GeoIP country code")
		}
		return "geoip", nil
	}
	if strings.HasPrefix(addr, "geosite:") {
		name := addr[len("geosite:"):]
		if i := strings.Index(name, "@"); i >= 0 {
			name = name[:i] // upstream parseGeoSiteName splits off @attrs
		}
		if strings.TrimSpace(name) == "" {
			return "", fmt.Errorf("empty GeoSite name")
		}
		return "geosite", nil
	}
	if strings.HasPrefix(addr, "suffix:") {
		if addr[len("suffix:"):] == "" {
			return "", fmt.Errorf("empty domain suffix")
		}
		return "suffix", nil
	}
	if strings.Contains(addr, "/") {
		if _, _, err := net.ParseCIDR(addr); err != nil {
			return "", fmt.Errorf("invalid CIDR address: %s", addr)
		}
		return "cidr", nil
	}
	if ip := net.ParseIP(addr); ip != nil {
		return "ip", nil
	}
	if strings.Contains(addr, "*") {
		return "wildcard", nil
	}
	return "domain", nil
}

// compileHysteriaACLInline parses then "compiles" every rendered inline rule
// the way hysteria server would: unknown outbounds and uncompilable addresses
// are errors. It returns the (outbound, matcher-kind) pairs for assertions.
func compileHysteriaACLInline(t *testing.T, inline []string) [][2]string {
	t.Helper()
	rules, err := hysteriaACLParseTextRules(strings.Join(inline, "\n"))
	if err != nil {
		t.Fatalf("hysteria ACL parse failed: %v", err)
	}
	compiled := make([][2]string, 0, len(rules))
	for _, rule := range rules {
		switch rule.outbound {
		case "direct", "proxy", "warp":
		default:
			t.Fatalf("hysteria ACL compile failed at line %d: outbound %s not found", rule.lineNum, rule.outbound)
		}
		kind, err := hysteriaACLCompileAddress(rule.address)
		if err != nil {
			t.Fatalf("hysteria ACL compile failed at line %d: %v", rule.lineNum, err)
		}
		compiled = append(compiled, [2]string{rule.outbound, kind})
	}
	return compiled
}

func hysteria2ACLInline(t *testing.T, rendered string) []string {
	t.Helper()
	var doc struct {
		ACL struct {
			Inline []string `yaml:"inline"`
		} `yaml:"acl"`
	}
	if err := yaml.Unmarshal([]byte(rendered), &doc); err != nil {
		t.Fatalf("rendered config is not valid YAML: %v\n%s", err, rendered)
	}
	return doc.ACL.Inline
}

// #680: every line the renderer emits must parse and compile under
// Hysteria's real ACL grammar — the false-green substring contract that let
// cidr:/regex: through is replaced by an actual compile gate.
func TestRenderHysteria2ACLCompilesUnderUpstreamGrammar(t *testing.T) {
	dir := t.TempDir()
	geoip := filepath.Join(dir, "geoip.dat")
	geosite := filepath.Join(dir, "geosite.dat")
	if err := os.WriteFile(geoip, []byte("geoip"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(geosite, []byte("geosite"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := RenderHysteria2(Hysteria2Config{
		ListenPort:  443,
		Password:    "secret",
		Upstream:    "127.0.0.1:40000",
		GeoIPPath:   geoip,
		GeoSitePath: geosite,
		RoutingRules: []Hysteria2RoutingRule{
			{Match: "geosite:category-gov-ru,full:api.example.com", Outbound: "direct"},
			{Match: "geoip:private,geoip:cn", Outbound: "direct"},
			{Match: "10.9.0.0/16,8.8.8.8", Outbound: "direct"},
			{Match: "domain:openai.com", Outbound: "warp"},
			{Match: "all", Outbound: "proxy"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	inline := hysteria2ACLInline(t, cfg)
	if len(inline) == 0 {
		t.Fatalf("expected acl.inline entries:\n%s", cfg)
	}
	compiled := compileHysteriaACLInline(t, inline)
	want := [][2]string{
		{"direct", "geosite"},
		{"direct", "domain"},
		{"direct", "geoip"},
		{"direct", "geoip"},
		{"direct", "cidr"},
		{"direct", "cidr"},
		{"warp", "suffix"},
		{"proxy", "all"},
	}
	if len(compiled) != len(want) {
		t.Fatalf("compiled %d rules %v, want %d: %v\ninline: %v", len(compiled), compiled, len(want), want, inline)
	}
	for i := range want {
		if compiled[i] != want[i] {
			t.Fatalf("compiled[%d] = %v, want %v\ninline: %v", i, compiled[i], want[i], inline)
		}
	}
}

// #680: the gate must be strong enough to have caught the old dialect —
// prove it rejects or misclassifies the exact lines tip used to emit.
func TestHysteriaACLGateCatchesOldDialect(t *testing.T) {
	// cidr: contains '/' so upstream takes the CIDR branch and ParseCIDR
	// fails on the prefix — a hard config-load crash.
	if _, err := hysteriaACLCompileAddress("cidr:1.2.3.0/24"); err == nil {
		t.Fatal("cidr:1.2.3.0/24 must fail upstream compile")
	}
	// keyword:/regex:/domain: prefixes are not stripped upstream: they compile
	// as wildcard/exact literal domains — silent wrong routing, no error.
	for addr, want := range map[string]string{
		"keyword:google":         "domain",
		"domain:api.example.com": "domain",
		"regex:.*\\.ru$":         "wildcard",
	} {
		kind, err := hysteriaACLCompileAddress(addr)
		if err != nil {
			t.Fatalf("%s unexpectedly failed compile: %v", addr, err)
		}
		if kind != want {
			t.Fatalf("%s compiled as %q, want %q (literal host, not the intended matcher)", addr, kind, want)
		}
	}
	// '#' is stripped as a comment before parse, leaving an unterminated line
	// → InvalidSyntax fails the entire ACL.
	if _, err := hysteriaACLParseTextRules("proxy(keyword:foo#bar)"); err == nil {
		t.Fatal("a # in the match value must fail upstream parse")
	}
	if _, err := hysteriaACLParseTextRules("warp(all)\nproxy(suffix:example.com)\n"); err != nil {
		t.Fatalf("native dialect must parse: %v", err)
	}
}

// #679: matchers with no Hysteria dialect, and values that would corrupt the
// line grammar, are omitted — never emitted as fake prefixes.
func TestRenderHysteria2ACLOmitsUnsupportedMatchers(t *testing.T) {
	cfg, err := RenderHysteria2(Hysteria2Config{
		ListenPort: 443,
		Password:   "secret",
		Upstream:   "127.0.0.1:40000",
		RoutingRules: []Hysteria2RoutingRule{
			{Match: `keyword:google,regexp:.*\.ru$,suffix:example.com`, Outbound: "direct"},
			{Match: `full:bad#host,full:ok.example.com`, Outbound: "direct"},
			{Match: "all", Outbound: "warp"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	inline := hysteria2ACLInline(t, cfg)
	compiled := compileHysteriaACLInline(t, inline)
	want := [][2]string{
		{"direct", "suffix"},
		{"direct", "domain"},
		{"warp", "all"},
	}
	if len(compiled) != len(want) {
		t.Fatalf("compiled %v, want %v\ninline: %v", compiled, want, inline)
	}
	for i := range want {
		if compiled[i] != want[i] {
			t.Fatalf("compiled[%d] = %v, want %v\ninline: %v", i, compiled[i], want[i], inline)
		}
	}
	for _, bad := range []string{"keyword:", "regex:", "regexp:", "domain:", "cidr:", "#"} {
		if strings.Contains(cfg, bad) {
			t.Fatalf("unsupported ACL fragment %q emitted:\n%s", bad, cfg)
		}
	}
}
