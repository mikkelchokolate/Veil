package api

import "errors"

var (
	ErrRoutingRuleInvalid       = errors.New("routing rule invalid")
	ErrRoutingRuleNotFound      = errors.New("routing rule not found")
	ErrRoutingRuleDuplicateName = errors.New("routing rule name already exists")
)

type RoutingRuleManagement struct {
	rules *[]RoutingRule
	save  func() error
}

func NewRoutingRuleManagement(rules *[]RoutingRule, save func() error) RoutingRuleManagement {
	if save == nil {
		save = func() error { return nil }
	}
	return RoutingRuleManagement{rules: rules, save: save}
}

func (m RoutingRuleManagement) List() []RoutingRule {
	if m.rules == nil {
		return nil
	}
	return append([]RoutingRule(nil), (*m.rules)...)
}

func (m RoutingRuleManagement) Get(name string) (RoutingRule, bool) {
	idx := m.index(name)
	if idx < 0 || m.rules == nil {
		return RoutingRule{}, false
	}
	return (*m.rules)[idx], true
}

func (m RoutingRuleManagement) Create(rule RoutingRule) (RoutingRule, error) {
	if rule.Name == "" || rule.Match == "" || rule.Outbound == "" {
		return RoutingRule{}, ErrRoutingRuleInvalid
	}
	if m.index(rule.Name) >= 0 {
		return RoutingRule{}, ErrRoutingRuleDuplicateName
	}
	next := append(m.List(), rule)
	if err := m.replaceAndSave(next); err != nil {
		return RoutingRule{}, err
	}
	return rule, nil
}

func (m RoutingRuleManagement) Update(name string, update RoutingRule) (RoutingRule, error) {
	idx := m.index(name)
	if idx < 0 {
		return RoutingRule{}, ErrRoutingRuleNotFound
	}
	if update.Match == "" || update.Outbound == "" {
		return RoutingRule{}, ErrRoutingRuleInvalid
	}
	update.Name = name
	next := m.List()
	next[idx] = update
	if err := m.replaceAndSave(next); err != nil {
		return RoutingRule{}, err
	}
	return update, nil
}

func (m RoutingRuleManagement) Delete(name string) error {
	idx := m.index(name)
	if idx < 0 {
		return ErrRoutingRuleNotFound
	}
	next := m.List()
	next = append(next[:idx], next[idx+1:]...)
	return m.replaceAndSave(next)
}

func (m RoutingRuleManagement) index(name string) int {
	if m.rules == nil {
		return -1
	}
	for idx, rule := range *m.rules {
		if rule.Name == name {
			return idx
		}
	}
	return -1
}

func (m RoutingRuleManagement) replaceAndSave(next []RoutingRule) error {
	if m.rules != nil {
		*m.rules = next
	}
	return m.save()
}
