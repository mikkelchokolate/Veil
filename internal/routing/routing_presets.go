package routing

const (
	routingRulesRepository = "https://github.com/runetfreedom/russia-v2ray-rules-dat"
	routingRulesRelease    = "202607301129"
)

func routeDatSource() RoutingSource {
	const releaseBase = routingRulesRepository + "/releases/download/" + routingRulesRelease
	return RoutingSource{
		Repository: routingRulesRepository + "/releases/tag/" + routingRulesRelease,
		Files: []RoutingSourceFile{
			{
				Name:         "geoip.dat",
				URL:          releaseBase + "/geoip.dat",
				SHA256URL:    releaseBase + "/geoip.dat.sha256sum",
				PinnedSHA256: "3aeb1cc31bbf0e490217bb2d14a1d207f372bfe92abff1b1c1a01ee59a2f2327",
			},
			{
				Name:         "geosite.dat",
				URL:          releaseBase + "/geosite.dat",
				SHA256URL:    releaseBase + "/geosite.dat.sha256sum",
				PinnedSHA256: "ae2b3e8375a00992a979d09c4bc28f14f15f39096349881d3b3c50ae3d1e269a",
			},
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
