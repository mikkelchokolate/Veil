package api

import "errors"

var (
	ErrRoutingRuleInvalid       = errors.New("routing rule invalid")
	ErrRoutingRuleNotFound      = errors.New("routing rule not found")
	ErrRoutingRuleDuplicateName = errors.New("routing rule name already exists")
)

type RoutingRuleManagement struct {
	mutation ManagementStateMutation
}

func NewRoutingRuleManagement(rules *[]RoutingRule, save func() error) RoutingRuleManagement {
	return RoutingRuleManagement{mutation: NewManagementStateMutation(ManagementStateMutationTarget{Rules: rules}, save)}
}

func (m RoutingRuleManagement) List() []RoutingRule {
	return m.mutation.RoutingRules()
}

func (m RoutingRuleManagement) Get(name string) (RoutingRule, bool) {
	return m.mutation.RoutingRule(name)
}

func (m RoutingRuleManagement) Create(rule RoutingRule) (RoutingRule, error) {
	return m.mutation.CreateRoutingRule(rule)
}

func (m RoutingRuleManagement) Update(name string, update RoutingRule) (RoutingRule, error) {
	return m.mutation.UpdateRoutingRule(name, update)
}

func (m RoutingRuleManagement) Delete(name string) error {
	return m.mutation.DeleteRoutingRule(name)
}
