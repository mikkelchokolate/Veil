package routing

import (
	"errors"
	"testing"
)

func TestRoutingRuleValidationCreateRequiresNameMatchAndOutbound(t *testing.T) {
	validator := NewRoutingRuleValidation()
	if err := validator.ValidateCreate(RoutingRule{Name: "rule", Match: "geoip:private", Outbound: "direct"}); err != nil {
		t.Fatalf("ValidateCreate valid: %v", err)
	}
	for _, rule := range []RoutingRule{{Match: "m", Outbound: "o"}, {Name: "n", Outbound: "o"}, {Name: "n", Match: "m"}} {
		if err := validator.ValidateCreate(rule); err != ErrRoutingRuleInvalid {
			t.Fatalf("ValidateCreate(%+v) = %v", rule, err)
		}
	}
}

func TestRoutingRuleValidationUpdateDoesNotRequireName(t *testing.T) {
	validator := NewRoutingRuleValidation()
	if err := validator.ValidateUpdate(RoutingRule{Match: "geoip:private", Outbound: "direct"}); err != nil {
		t.Fatalf("ValidateUpdate valid: %v", err)
	}
	if err := validator.ValidateUpdate(RoutingRule{Outbound: "direct"}); err != ErrRoutingRuleInvalid {
		t.Fatalf("ValidateUpdate invalid = %v", err)
	}
}

// #946: ParseMatch is wired into BOTH validation arms — a rule whose required
// fields are all present but whose match expression is malformed must fail
// with the match-specific error (ErrRoutingMatchInvalid), not the generic
// required-fields error. The match gate must not be bypassed on update either.
func TestRoutingRuleValidationRejectsInvalidMatchOnCreateAndUpdate(t *testing.T) {
	validator := NewRoutingRuleValidation()
	invalidMatches := []string{
		`regexp:(`,            // uncompilable regexp atom
		`geosite:`,            // empty geosite code
		`geoip:not a code`,    // invalid geoip characters
		`cidr:not-an-ip`,      // unparseable CIDR
		`keyword:`,            // empty keyword
		`a b`,                 // bare token with whitespace
		`geosite:ok,regexp:(`, // second atom invalid inside a list
	}
	for _, match := range invalidMatches {
		createErr := validator.ValidateCreate(RoutingRule{Name: "n", Match: match, Outbound: "direct"})
		if !errors.Is(createErr, ErrRoutingMatchInvalid) {
			t.Fatalf("ValidateCreate(match=%q) = %v, want ErrRoutingMatchInvalid", match, createErr)
		}
		updateErr := validator.ValidateUpdate(RoutingRule{Name: "n", Match: match, Outbound: "direct"})
		if !errors.Is(updateErr, ErrRoutingMatchInvalid) {
			t.Fatalf("ValidateUpdate(match=%q) = %v, want ErrRoutingMatchInvalid", match, updateErr)
		}
	}
}

// #1410: outbound is a closed enum. The apply planner only accepts exactly
// direct, proxy and warp (case-sensitive), so the write path must reject
// anything else with ErrRoutingOutboundInvalid instead of persisting a rule
// that blocks every later apply.
func TestRoutingRuleValidationRejectsUnknownOutboundOnCreateAndUpdate(t *testing.T) {
	validator := NewRoutingRuleValidation()
	for _, outbound := range []string{"direct", "proxy", "warp"} {
		rule := RoutingRule{Name: "n", Match: "geoip:private", Outbound: outbound}
		if err := validator.ValidateCreate(rule); err != nil {
			t.Fatalf("ValidateCreate(outbound=%q) = %v, want nil", outbound, err)
		}
		if err := validator.ValidateUpdate(rule); err != nil {
			t.Fatalf("ValidateUpdate(outbound=%q) = %v, want nil", outbound, err)
		}
	}
	for _, outbound := range []string{"Direct", "WARP", " warp", "proxy ", "block", "evil", "blackhole"} {
		rule := RoutingRule{Name: "n", Match: "geoip:private", Outbound: outbound}
		if err := validator.ValidateCreate(rule); !errors.Is(err, ErrRoutingOutboundInvalid) {
			t.Fatalf("ValidateCreate(outbound=%q) = %v, want ErrRoutingOutboundInvalid", outbound, err)
		}
		if err := validator.ValidateUpdate(rule); !errors.Is(err, ErrRoutingOutboundInvalid) {
			t.Fatalf("ValidateUpdate(outbound=%q) = %v, want ErrRoutingOutboundInvalid", outbound, err)
		}
		if IsValidOutbound(outbound) {
			t.Fatalf("IsValidOutbound(%q) = true", outbound)
		}
	}
	// An empty outbound is still the generic required-field error.
	if err := validator.ValidateCreate(RoutingRule{Name: "n", Match: "geoip:private"}); err != ErrRoutingRuleInvalid {
		t.Fatalf("ValidateCreate(empty outbound) = %v, want ErrRoutingRuleInvalid", err)
	}
}

// Outbounds() hands out a copy: mutating it must not widen the allowlist.
func TestOutboundsReturnsCopy(t *testing.T) {
	got := Outbounds()
	got[0] = "evil"
	if IsValidOutbound("evil") || Outbounds()[0] != "direct" {
		t.Fatalf("mutating Outbounds() changed the allowlist: %v", Outbounds())
	}
}
