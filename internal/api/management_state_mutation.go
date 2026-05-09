package api

import "github.com/veil-panel/veil/internal/managementstate"

type ManagementStateMutationTarget = managementstate.MutationTarget
type ManagementStateMutation = managementstate.Mutation

func NewManagementStateMutation(target ManagementStateMutationTarget, save func() error) ManagementStateMutation {
	return managementstate.NewMutation(target, save)
}

func NewManagementStateMutationFromState(state *managementState) ManagementStateMutation {
	if state == nil {
		return NewManagementStateMutation(ManagementStateMutationTarget{}, nil)
	}
	return NewManagementStateMutation(ManagementStateMutationTarget{Settings: &state.settings, Inbounds: &state.inbounds, Rules: &state.rules, Warp: &state.warp}, state.saveLocked)
}
