package api

type ManagementStateLifecycle struct {
	state *managementState
}

func NewManagementStateLifecycle(state *managementState) ManagementStateLifecycle {
	return ManagementStateLifecycle{state: state}
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

func ApplyManagementSnapshot(state *managementState, snapshot managementSnapshot) {
	if state == nil {
		return
	}
	if snapshot.Settings.PanelListen != "" {
		state.settings = snapshot.Settings
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
