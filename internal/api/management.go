package api

import (
	"net/http"
	"path/filepath"
)

var stagedConfigValidator = runStagedConfigValidators
var serviceActionRunner = runFixedServiceAction
var serviceHealthChecker = runFixedServiceHealthCheck

// Reloader is an optional interface for runtime state reload.
type Reloader interface {
	Reload() error
}

func (s *managementState) register(mux *http.ServeMux) {
	ManagementRoutes{}.Register(mux, s)
}

func (s *managementState) applyHistoryPathLocked() string {
	return filepath.Join(s.applyRoot, "generated", "veil", "apply-history.json")
}

func (s *managementState) loadApplyHistoryLocked() ([]ApplyHistoryEntry, error) {
	return NewApplyHistoryStore(s.applyHistoryPathLocked(), maxApplyHistoryEntries).Load()
}

func (s *managementState) livePathForStagedConfig(stagedPath string) (string, bool) {
	context := NewManagementApplyContext(s)
	return NewLiveConfigPromotion(s.applyRoot, context.reloadPromotedServicesLocked).LivePathForStagedConfig(stagedPath)
}

func (s *managementState) renderManagementConfigsLocked() (map[string]string, error) {
	return s.managementConfigRendererLocked().Render()
}

func (s *managementState) hasRenderSettingsLocked() bool {
	return s.managementConfigRendererLocked().HasRenderSettings()
}

func (s *managementState) renderNaiveConfigLocked(inbound Inbound) (string, error) {
	return s.managementConfigRendererLocked().RenderInbound(inbound)
}

func (s *managementState) renderHysteria2ConfigLocked(inbound Inbound) (string, error) {
	return s.managementConfigRendererLocked().RenderInbound(inbound)
}

func (s *managementState) renderWarpConfigLocked() (string, error) {
	return s.managementConfigRendererLocked().RenderWarp()
}

func (s *managementState) managementConfigRendererLocked() ManagementConfigRenderer {
	return NewManagementConfigRenderer(ManagementConfigInput{
		ApplyRoot: s.applyRoot,
		Settings:  s.settings,
		Inbounds:  s.inbounds,
		Rules:     s.rules,
		Warp:      s.warp,
	})
}

func (s *managementState) snapshotLocked() managementSnapshot {
	return NewManagementStateLifecycle(s).SnapshotLocked()
}

func (s *managementState) encryptSnapshot(snapshot *managementSnapshot) {
	EncryptManagementSnapshot(snapshot, s.cipher)
}

func (s *managementState) decryptSnapshot(snapshot *managementSnapshot) {
	DecryptManagementSnapshot(snapshot, s.cipher)
}

func (s *managementState) load() error {
	return NewManagementStateLifecycle(s).Load()
}

// Reload re-reads the management state and encryption key from disk.
// It locks the state mutex during the reload. Returns an error if the
// state file or key file cannot be read, but leaves the previous state
// intact on failure.
func (s *managementState) Reload() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return NewManagementStateLifecycle(s).ReloadLocked()
}

func (s *managementState) saveLocked() error {
	return NewManagementStateLifecycle(s).SaveLocked()
}

func buildFirewallRules(settings Settings, inbounds []Inbound) []firewallRuleResponse {
	return BuildFirewallRuleResponses(settings, inbounds)
}
