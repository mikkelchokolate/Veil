package api

import veilwarp "github.com/veil-panel/veil/internal/warp"

type WarpManagement struct {
	mutation ManagementStateMutation
}

func NewWarpManagement(warp *WarpConfig, save func() error) WarpManagement {
	return WarpManagement{mutation: NewManagementStateMutation(ManagementStateMutationTarget{Warp: warp}, save)}
}

func (m WarpManagement) Get() WarpConfig {
	return m.mutation.Warp()
}

func (m WarpManagement) Update(update WarpConfig) (WarpConfig, error) {
	return m.mutation.UpdateWarp(update)
}

func redactedWarp(warp WarpConfig) WarpConfig {
	return veilwarp.Redact(warp)
}

func setWarpDefaults(warp *WarpConfig) {
	veilwarp.SetDefaults(warp)
}
