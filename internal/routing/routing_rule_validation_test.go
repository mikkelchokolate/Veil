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
