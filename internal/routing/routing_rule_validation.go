package routing

type RoutingRuleValidation struct{}

func NewRoutingRuleValidation() RoutingRuleValidation { return RoutingRuleValidation{} }

// Outbounds is the closed set of routing outbounds the apply planner accepts.
// Matching is exact and case-sensitive, mirroring applyplan (#1410).
var Outbounds = []string{"direct", "proxy", "warp"}

// IsValidOutbound reports whether outbound is one of Outbounds exactly.
func IsValidOutbound(outbound string) bool {
	for _, allowed := range Outbounds {
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
