package api

type RoutingRuleValidation struct{}

func NewRoutingRuleValidation() RoutingRuleValidation { return RoutingRuleValidation{} }

func (RoutingRuleValidation) ValidateCreate(rule RoutingRule) error {
	if rule.Name == "" || rule.Match == "" || rule.Outbound == "" {
		return ErrRoutingRuleInvalid
	}
	return nil
}

func (RoutingRuleValidation) ValidateUpdate(rule RoutingRule) error {
	if rule.Match == "" || rule.Outbound == "" {
		return ErrRoutingRuleInvalid
	}
	return nil
}
