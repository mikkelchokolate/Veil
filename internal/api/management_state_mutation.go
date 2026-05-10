package api

import "github.com/veil-panel/veil/internal/managementstate"

func newManagementStateMutationFromState(state *managementState) managementstate.Mutation {
	if state == nil {
		return managementstate.NewMutation(managementstate.MutationTarget{}, nil)
	}
	return managementstate.NewMutation(managementstate.MutationTarget{Settings: &state.settings, Inbounds: &state.inbounds, Rules: &state.rules, Warp: &state.warp}, state.saveLocked)
}
