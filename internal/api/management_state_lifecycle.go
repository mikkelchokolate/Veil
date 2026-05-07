package api

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
