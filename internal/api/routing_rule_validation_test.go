package api

import "testing"

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
