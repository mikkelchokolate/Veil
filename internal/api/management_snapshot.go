package api

import "github.com/veil-panel/veil/internal/secrets"

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
		Inbounds:      append([]Inbound(nil), input.Inbounds...),
		Rules:         append([]RoutingRule(nil), input.Rules...),
		RoutingPreset: input.RoutingPreset,
		RoutingSource: input.RoutingSource,
		Warp:          input.Warp,
	}
}

func EncryptManagementSnapshot(snapshot *managementSnapshot, cipher *secrets.Cipher) {
	NewStateStore("", cipher).encryptSnapshot(snapshot)
}

func DecryptManagementSnapshot(snapshot *managementSnapshot, cipher *secrets.Cipher) {
	NewStateStore("", cipher).decryptSnapshot(snapshot)
}
