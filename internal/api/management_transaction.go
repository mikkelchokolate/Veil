package api

type managementTransaction struct {
	state *managementState
}

func (s *managementState) withTransaction(fn func(*managementTransaction) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fn(&managementTransaction{state: s})
}

func (tx *managementTransaction) Mutation() ManagementStateMutation {
	return NewManagementStateMutationFromState(tx.state)
}

func (tx *managementTransaction) Settings() SettingsManagement {
	return SettingsManagement{mutation: tx.Mutation()}
}

func (tx *managementTransaction) Inbounds() InboundManagement {
	return InboundManagement{mutation: tx.Mutation()}
}

func (tx *managementTransaction) RoutingRules() RoutingRuleManagement {
	return RoutingRuleManagement{mutation: tx.Mutation()}
}

func (tx *managementTransaction) Warp() WarpManagement {
	return WarpManagement{mutation: tx.Mutation()}
}
