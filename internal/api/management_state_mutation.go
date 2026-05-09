package api

type ManagementStateMutationTarget struct {
	Settings *Settings
	Inbounds *[]Inbound
	Rules    *[]RoutingRule
	Warp     *WarpConfig
}

type ManagementStateMutation struct {
	target ManagementStateMutationTarget
	save   func() error
}

func NewManagementStateMutation(target ManagementStateMutationTarget, save func() error) ManagementStateMutation {
	if save == nil {
		save = func() error { return nil }
	}
	return ManagementStateMutation{target: target, save: save}
}

func NewManagementStateMutationFromState(state *managementState) ManagementStateMutation {
	if state == nil {
		return NewManagementStateMutation(ManagementStateMutationTarget{}, nil)
	}
	return NewManagementStateMutation(ManagementStateMutationTarget{Settings: &state.settings, Inbounds: &state.inbounds, Rules: &state.rules, Warp: &state.warp}, state.saveLocked)
}

func (m ManagementStateMutation) Settings() Settings {
	if m.target.Settings == nil {
		return Settings{}
	}
	return redactedSettings(*m.target.Settings)
}

func (m ManagementStateMutation) UpdateSettings(update Settings) (Settings, error) {
	current := Settings{}
	if m.target.Settings != nil {
		current = *m.target.Settings
	}
	if err := normalizeAndValidateSettings(&update, current); err != nil {
		return Settings{}, err
	}
	if m.target.Settings != nil {
		*m.target.Settings = update
	}
	if err := m.save(); err != nil {
		return Settings{}, err
	}
	return redactedSettings(update), nil
}

func (m ManagementStateMutation) Inbounds() []Inbound {
	if m.target.Inbounds == nil {
		return nil
	}
	return NewInboundCatalog(*m.target.Inbounds).List()
}

func (m ManagementStateMutation) Inbound(name string) (Inbound, bool) {
	if m.target.Inbounds == nil {
		return Inbound{}, false
	}
	return NewInboundCatalog(*m.target.Inbounds).Get(name)
}

func (m ManagementStateMutation) CreateInbound(inbound Inbound) (Inbound, error) {
	catalog := NewInboundCatalog(m.Inbounds())
	created, next, err := catalog.Create(inbound)
	if err != nil {
		return Inbound{}, err
	}
	if err := m.replaceInbounds(next.List()); err != nil {
		return Inbound{}, err
	}
	return created, nil
}

func (m ManagementStateMutation) UpdateInbound(name string, update Inbound) (Inbound, error) {
	catalog := NewInboundCatalog(m.Inbounds())
	updated, next, err := catalog.Update(name, update)
	if err != nil {
		return Inbound{}, err
	}
	if err := m.replaceInbounds(next.List()); err != nil {
		return Inbound{}, err
	}
	return updated, nil
}

func (m ManagementStateMutation) DeleteInbound(name string) error {
	catalog := NewInboundCatalog(m.Inbounds())
	next, err := catalog.Delete(name)
	if err != nil {
		return err
	}
	return m.replaceInbounds(next.List())
}

func (m ManagementStateMutation) replaceInbounds(next []Inbound) error {
	if m.target.Inbounds != nil {
		*m.target.Inbounds = next
	}
	return m.save()
}

func (m ManagementStateMutation) RoutingRules() []RoutingRule {
	if m.target.Rules == nil {
		return nil
	}
	return append([]RoutingRule(nil), (*m.target.Rules)...)
}

func (m ManagementStateMutation) RoutingRule(name string) (RoutingRule, bool) {
	idx := m.routingRuleIndex(name)
	if idx < 0 || m.target.Rules == nil {
		return RoutingRule{}, false
	}
	return (*m.target.Rules)[idx], true
}

func (m ManagementStateMutation) CreateRoutingRule(rule RoutingRule) (RoutingRule, error) {
	if err := NewRoutingRuleValidation().ValidateCreate(rule); err != nil {
		return RoutingRule{}, err
	}
	if m.routingRuleIndex(rule.Name) >= 0 {
		return RoutingRule{}, ErrRoutingRuleDuplicateName
	}
	next := append(m.RoutingRules(), rule)
	if err := m.replaceRoutingRules(next); err != nil {
		return RoutingRule{}, err
	}
	return rule, nil
}

func (m ManagementStateMutation) UpdateRoutingRule(name string, update RoutingRule) (RoutingRule, error) {
	idx := m.routingRuleIndex(name)
	if idx < 0 {
		return RoutingRule{}, ErrRoutingRuleNotFound
	}
	if err := NewRoutingRuleValidation().ValidateUpdate(update); err != nil {
		return RoutingRule{}, err
	}
	update.Name = name
	next := m.RoutingRules()
	next[idx] = update
	if err := m.replaceRoutingRules(next); err != nil {
		return RoutingRule{}, err
	}
	return update, nil
}

func (m ManagementStateMutation) DeleteRoutingRule(name string) error {
	idx := m.routingRuleIndex(name)
	if idx < 0 {
		return ErrRoutingRuleNotFound
	}
	next := m.RoutingRules()
	next = append(next[:idx], next[idx+1:]...)
	return m.replaceRoutingRules(next)
}

func (m ManagementStateMutation) routingRuleIndex(name string) int {
	if m.target.Rules == nil {
		return -1
	}
	return NewRoutingRuleIndex(*m.target.Rules).Index(name)
}

func (m ManagementStateMutation) replaceRoutingRules(next []RoutingRule) error {
	if m.target.Rules != nil {
		*m.target.Rules = next
	}
	return m.save()
}

func (m ManagementStateMutation) Warp() WarpConfig {
	if m.target.Warp == nil {
		return WarpConfig{}
	}
	return redactedWarp(*m.target.Warp)
}

func (m ManagementStateMutation) UpdateWarp(update WarpConfig) (WarpConfig, error) {
	current := WarpConfig{}
	if m.target.Warp != nil {
		current = *m.target.Warp
	}
	disclosure := NewCredentialDisclosure()
	update.LicenseKey = disclosure.PreserveRedacted(update.LicenseKey, current.LicenseKey)
	update.PrivateKey = disclosure.PreserveRedacted(update.PrivateKey, current.PrivateKey)
	setWarpDefaults(&update)
	if m.target.Warp != nil {
		*m.target.Warp = update
	}
	if err := m.save(); err != nil {
		return WarpConfig{}, err
	}
	return redactedWarp(update), nil
}
