package api

import (
	"github.com/veil-panel/veil/internal/managementstate"
	"github.com/veil-panel/veil/internal/secrets"
)

type ManagementSnapshotInput struct {
	Settings      Settings
	Inbounds      []Inbound
	Rules         []RoutingRule
	RoutingPreset string
	RoutingSource RoutingSource
	Warp          WarpConfig
}

func BuildManagementSnapshot(input ManagementSnapshotInput) managementSnapshot {
	return managementSnapshot{
		Settings:      input.Settings,
		Inbounds:      cloneInbounds(input.Inbounds),
		Rules:         append([]RoutingRule(nil), input.Rules...),
		RoutingPreset: input.RoutingPreset,
		RoutingSource: input.RoutingSource,
		Warp:          input.Warp,
	}
}

func EncryptManagementSnapshot(snapshot *managementSnapshot, cipher *secrets.Cipher) {
	managementstate.EncryptSnapshot(snapshot, cipher)
}

func DecryptManagementSnapshot(snapshot *managementSnapshot, cipher *secrets.Cipher) {
	managementstate.DecryptSnapshot(snapshot, cipher)
}
