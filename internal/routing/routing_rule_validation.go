package routing

type RoutingRuleValidation struct{}

func NewRoutingRuleValidation() RoutingRuleValidation { return RoutingRuleValidation{} }

// outbounds is the closed set of routing outbounds the apply planner accepts.
// Matching is exact and case-sensitive, mirroring applyplan (#1410). It is
// unexported so no caller can widen the write-path allowlist at runtime.
var outbounds = [...]string{"direct", "proxy", "warp"}

// Outbounds returns a copy of the closed set of accepted routing outbounds.
func Outbounds() []string {
	out := outbounds
	return out[:]
}

// IsValidOutbound reports whether outbound is exactly one of Outbounds().
func IsValidOutbound(outbound string) bool {
	for _, allowed := range outbounds {
		if outbound == allowed {
			return true
		}
	}
	return false
}

func (RoutingRuleValidation) ValidateCreate(rule RoutingRule) error {
	if rule.Name == "" || rule.Match == "" || rule.Outbound == "" {
		return ErrRoutingRuleInvalid
	}
	return validateRuleFields(rule)
}

func (RoutingRuleValidation) ValidateUpdate(rule RoutingRule) error {
	if rule.Match == "" || rule.Outbound == "" {
		return ErrRoutingRuleInvalid
	}
	return validateRuleFields(rule)
}

func validateRuleFields(rule RoutingRule) error {
	if !IsValidOutbound(rule.Outbound) {
		return ErrRoutingOutboundInvalid
	}
	if _, err := ParseMatch(rule.Match); err != nil {
		return err
	}
	return nil
}
