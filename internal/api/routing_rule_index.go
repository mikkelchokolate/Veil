package api

type RoutingRuleIndex struct {
	rules []RoutingRule
}

func NewRoutingRuleIndex(rules []RoutingRule) RoutingRuleIndex {
	return RoutingRuleIndex{rules: rules}
}

func (i RoutingRuleIndex) Index(name string) int {
	for idx, rule := range i.rules {
		if rule.Name == name {
			return idx
		}
	}
	return -1
}

func (i RoutingRuleIndex) Has(name string) bool { return i.Index(name) >= 0 }
