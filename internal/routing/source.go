package routing

// EnsureDatSource attaches the default geoip/geosite material when enabled
// rules need it and the current source has no files. Custom rules (not a
// preset) otherwise leave routingSource empty, so geosite: matchers are
// silently dropped at render time.
func EnsureDatSource(current RoutingSource, rules []RoutingRule) RoutingSource {
	if len(current.Files) > 0 {
		return current
	}
	if !rulesNeedRouteDat(rules) {
		return current
	}
	return routeDatSource()
}

func rulesNeedRouteDat(rules []RoutingRule) bool {
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		matchers, err := ParseMatch(rule.Match)
		if err != nil {
			continue
		}
		for _, matcher := range matchers {
			switch matcher.Kind {
			case MatchGeoIP, MatchGeoSite, MatchPrivateIP:
				return true
			}
		}
	}
	return false
}
