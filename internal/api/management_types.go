package api

import (
	"sync"

	"github.com/veil-panel/veil/internal/model"
	"github.com/veil-panel/veil/internal/secrets"
)

type Settings = model.Settings
type ClientProfile = model.ClientProfile
type Inbound = model.Inbound
type RoutingRule = model.RoutingRule
type RoutingPreset = model.RoutingPreset
type RoutingPresetResponse = model.RoutingPresetResponse
type RoutingSource = model.RoutingSource
type RoutingSourceFile = model.RoutingSourceFile
type WarpConfig = model.WarpConfig
type ClientLinksResponse = model.ClientLinksResponse
type ClientLink = model.ClientLink
type ClientArtifact = model.ClientArtifact
type ApplyPlanResponse = model.ApplyPlanResponse
type ApplyRequest = model.ApplyRequest
type ApplyResponse = model.ApplyResponse
type ApplyHistoryEntry = model.ApplyHistoryEntry
type ConfigValidationResult = model.ConfigValidationResult
type ServiceActionResult = model.ServiceActionResult
type ServiceHealthResult = model.ServiceHealthResult

type livePromotionRecord struct {
	LivePath    string
	BackupPath  string
	HadPrevious bool
}

type managementSnapshot = model.ManagementSnapshot

type managementState struct {
	mu            sync.Mutex
	statePath     string
	applyRoot     string
	keyPath       string
	cipher        *secrets.Cipher
	settings      Settings
	inbounds      []Inbound
	rules         []RoutingRule
	routingPreset string
	routingSource RoutingSource
	warp          WarpConfig
}
