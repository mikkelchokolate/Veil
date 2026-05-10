package api

import "github.com/veil-panel/veil/internal/managementstate"

type managementTransaction struct {
	state *managementState
}

func (s *managementState) withTransaction(fn func(*managementTransaction) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fn(&managementTransaction{state: s})
}

func (tx *managementTransaction) Mutation() managementstate.Mutation {
	if tx.state == nil {
		return managementstate.NewMutation(managementstate.MutationTarget{}, nil)
	}
	state := tx.state
	return managementstate.NewMutation(managementstate.MutationTarget{Settings: &state.settings, Inbounds: &state.inbounds, Rules: &state.rules, Warp: &state.warp}, state.saveLocked)
}
