package routing

const routingRulesRepository = "https://github.com/runetfreedom/russia-v2ray-rules-dat"

func routeDatSource() RoutingSource {
	return RoutingSource{
		Repository: routingRulesRepository,
		Files: []RoutingSourceFile{
			{Name: "geoip.dat", URL: "https://raw.githubusercontent.com/runetfreedom/russia-v2ray-rules-dat/release/geoip.dat", SHA256URL: "https://github.com/runetfreedom/russia-v2ray-rules-dat/releases/latest/download/geoip.dat.sha256sum"},
			{Name: "geosite.dat", URL: "https://raw.githubusercontent.com/runetfreedom/russia-v2ray-rules-dat/release/geosite.dat", SHA256URL: "https://github.com/runetfreedom/russia-v2ray-rules-dat/releases/latest/download/geosite.dat.sha256sum"},
		},
	}
}

func routingPresetProfiles() []RoutingPreset {
	source := routeDatSource()
	return []RoutingPreset{
		{
			Name:        "all",
			Description: "Route all traffic through proxy.",
			Rules:       []RoutingRule{{Name: "preset-all-through-proxy", Match: "all", Outbound: "proxy", Enabled: true}},
		},
		{
			Name:        "all-except-Russia",
			Description: "Route Russian geo/site categories direct and everything else through proxy.",
			Source:      source,
			Rules: []RoutingRule{
				{Name: "preset-all-except-russia-private", Match: "geoip:private", Outbound: "direct", Enabled: true},
				{Name: "preset-all-except-russia-geoip", Match: "geoip:ru", Outbound: "direct", Enabled: true},
				{Name: "preset-all-except-russia-geosite", Match: "geosite:category-ru", Outbound: "direct", Enabled: true},
				{Name: "preset-all-except-russia-rest", Match: "all", Outbound: "proxy", Enabled: true},
			},
		},
		{
			Name:        "RU-blocked",
			Description: "Route domains and IPs blocked in Russia through proxy; leave everything else direct.",
			Source:      source,
			Rules: []RoutingRule{
				{Name: "preset-ru-blocked-geoip", Match: "geoip:ru-blocked", Outbound: "proxy", Enabled: true},
				{Name: "preset-ru-blocked-geosite", Match: "geosite:ru-blocked", Outbound: "proxy", Enabled: true},
			},
		},
	}
}

func routingPresetByName(name string) (RoutingPreset, bool) {
	for _, preset := range routingPresetProfiles() {
		if preset.Name == name {
			return preset, true
		}
	}
	return RoutingPreset{}, false
}
