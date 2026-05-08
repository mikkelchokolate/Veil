package api

import (
	"fmt"
	"log"

	"github.com/veil-panel/veil/internal/secrets"
)

type ManagementStateLifecycle struct {
	state *managementState
}

func NewManagementStateLifecycle(state *managementState) ManagementStateLifecycle {
	return ManagementStateLifecycle{state: state}
}

func newManagementState(info ServerInfo) *managementState {
	keyPath := info.KeyPath
	if keyPath == "" {
		keyPath = "/etc/veil/state.key"
	}
	model := defaultManagementModel(info)
	state := &managementState{
		statePath: info.StatePath,
		applyRoot: defaultApplyRoot(info.ApplyRoot),
		keyPath:   keyPath,
		settings:  model.Settings,
		inbounds:  model.Inbounds,
		rules:     model.Rules,
		warp:      model.Warp,
	}
	lifecycle := NewManagementStateLifecycle(state)
	if err := lifecycle.loadOrCreateCipher(); err != nil {
		log.Printf("error loading encryption key from %s: %v", keyPath, err)
	}
	if err := lifecycle.Load(); err != nil {
		log.Printf("error loading management state from %s: %v", info.StatePath, err)
	}
	return state
}

func (l ManagementStateLifecycle) loadOrCreateCipher() error {
	key, err := secrets.LoadOrCreateKey(l.state.keyPath)
	if err != nil {
		return err
	}
	cipher, err := secrets.NewCipher(*key)
	if err != nil {
		return err
	}
	l.state.cipher = cipher
	return nil
}

func (l ManagementStateLifecycle) SnapshotLocked() managementSnapshot {
	return BuildManagementSnapshot(ManagementSnapshotInput{
		Settings:      l.state.settings,
		Inbounds:      l.state.inbounds,
		Rules:         l.state.rules,
		RoutingPreset: l.state.routingPreset,
		RoutingSource: l.state.routingSource,
		Warp:          l.state.warp,
	})
}

func (l ManagementStateLifecycle) SaveLocked() error {
	return NewStateStore(l.state.statePath, l.state.cipher).Save(l.SnapshotLocked())
}

func (l ManagementStateLifecycle) Load() error {
	snapshot, ok, err := NewStateStore(l.state.statePath, l.state.cipher).Load()
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	ApplyManagementSnapshot(l.state, snapshot)
	return nil
}

func (l ManagementStateLifecycle) ReloadLocked() error {
	if l.state.keyPath != "" {
		key, err := secrets.LoadOrCreateKey(l.state.keyPath)
		if err != nil {
			return fmt.Errorf("reload key: %w", err)
		}
		cipher, err := secrets.NewCipher(*key)
		if err != nil {
			return fmt.Errorf("reload cipher: %w", err)
		}
		l.state.cipher = cipher
	}
	if l.state.statePath != "" {
		if err := l.Load(); err != nil {
			return fmt.Errorf("reload state: %w", err)
		}
	}
	return nil
}

func ApplyManagementSnapshot(state *managementState, snapshot managementSnapshot) {
	if state == nil {
		return
	}
	if snapshot.Settings.PanelListen != "" {
		state.settings = mergeSettingsDefaults(snapshot.Settings, state.settings)
	}
	if snapshot.Inbounds != nil {
		state.inbounds = snapshot.Inbounds
	}
	if snapshot.Rules != nil {
		state.rules = snapshot.Rules
	}
	if snapshot.RoutingPreset != "" {
		state.routingPreset = snapshot.RoutingPreset
	}
	if snapshot.RoutingSource.Repository != "" || len(snapshot.RoutingSource.Files) > 0 {
		state.routingSource = snapshot.RoutingSource
	}
	if snapshot.Warp.Endpoint != "" || snapshot.Warp.Enabled || snapshot.Warp.LicenseKey != "" {
		state.warp = snapshot.Warp
	}
}

func defaultApplyRoot(root string) string {
	if root != "" {
		return root
	}
	return "/etc/veil"
}
