package routing

type RoutingRuleValidation struct{}

func NewRoutingRuleValidation() RoutingRuleValidation { return RoutingRuleValidation{} }

func (RoutingRuleValidation) ValidateCreate(rule RoutingRule) error {
	if rule.Name == "" || rule.Match == "" || rule.Outbound == "" {
		return ErrRoutingRuleInvalid
	}
	if _, err := ParseMatch(rule.Match); err != nil {
		return err
	}
	return nil
}

func (RoutingRuleValidation) ValidateUpdate(rule RoutingRule) error {
	if rule.Match == "" || rule.Outbound == "" {
		return ErrRoutingRuleInvalid
	}
	if _, err := ParseMatch(rule.Match); err != nil {
		return err
	}
	return nil
}
