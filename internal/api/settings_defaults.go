package api

func mergeSettingsDefaults(settings Settings, defaults Settings) Settings {
	merged := settings
	if merged.PanelListen == "" {
		merged.PanelListen = defaults.PanelListen
	}
	if merged.PanelAccess == "" {
		merged.PanelAccess = defaults.PanelAccess
	}
	if merged.WebBasePath == "" {
		merged.WebBasePath = defaults.WebBasePath
	}
	if merged.Domain == "" {
		merged.Domain = defaults.Domain
	}
	if merged.Email == "" {
		merged.Email = defaults.Email
	}
	return merged
}
