package api

type managementTransaction struct {
	state *managementState
}

func (s *managementState) withTransaction(fn func(*managementTransaction) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fn(&managementTransaction{state: s})
}

func (tx *managementTransaction) Settings() SettingsManagement {
	return NewSettingsManagement(&tx.state.settings, tx.state.saveLocked)
}

func (tx *managementTransaction) Inbounds() InboundManagement {
	return NewInboundManagement(&tx.state.inbounds, tx.state.saveLocked)
}

func (tx *managementTransaction) RoutingRules() RoutingRuleManagement {
	return NewRoutingRuleManagement(&tx.state.rules, tx.state.saveLocked)
}

func (tx *managementTransaction) Warp() WarpManagement {
	return NewWarpManagement(&tx.state.warp, tx.state.saveLocked)
}
