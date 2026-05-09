package api

import "github.com/veil-panel/veil/internal/managementstate"

func mergeSettingsDefaults(settings Settings, defaults Settings) Settings {
	return managementstate.MergeSettingsDefaults(settings, defaults)
}
