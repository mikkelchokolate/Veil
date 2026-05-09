package routing

import "testing"

func TestRoutingRuleValidationAndIndex(t *testing.T) {
	validator := NewRuleValidation()
	if err := validator.ValidateCreate(Rule{Name: "private", Match: "geoip:private", Outbound: "direct"}); err != nil {
		t.Fatalf("ValidateCreate: %v", err)
	}
	if err := validator.ValidateCreate(Rule{Name: "missing"}); err != ErrRuleInvalid {
		t.Fatalf("expected ErrRuleInvalid, got %v", err)
	}
	index := NewRuleIndex([]Rule{{Name: "private"}})
	if !index.Has("private") || index.Has("missing") {
		t.Fatalf("unexpected index lookup")
	}
}
