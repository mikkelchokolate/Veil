package api

import "github.com/veil-panel/veil/internal/managementstate"

type managementModel = managementstate.DefaultState

func defaultManagementModel(info ServerInfo) managementModel {
	return managementstate.BuildDefaultState(managementstate.DefaultInput{
		PanelListen: info.PanelListen,
		PanelAccess: info.PanelAccess,
		WebBasePath: info.WebBasePath,
		Mode:        info.Mode,
		Domain:      info.Domain,
		Email:       info.Email,
	})
}
