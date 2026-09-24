package routing

// EnsureDatSource attaches the default geoip/geosite material when enabled
// rules need it and the current source has no files. Custom rules (not a
// preset) otherwise leave routingSource empty, so geosite: matchers are
// silently dropped at render time. Only the dat files the enabled matchers
// actually consume are attached: geoip matchers (including geoip:private)
// pull geoip.dat, geosite matchers pull geosite.dat — a private-only rule set
// must not download the ~74MiB geosite.dat it never reads (#764).
func EnsureDatSource(current RoutingSource, rules []RoutingRule) RoutingSource {
	if len(current.Files) > 0 {
		return current
	}
	needGeoIP, needGeoSite := rulesNeedRouteDat(rules)
	if !needGeoIP && !needGeoSite {
		return current
	}
	source := routeDatSource()
	files := make([]RoutingSourceFile, 0, len(source.Files))
	for _, file := range source.Files {
		switch file.Name {
		case "geoip.dat":
			if needGeoIP {
				files = append(files, file)
			}
		case "geosite.dat":
			if needGeoSite {
				files = append(files, file)
			}
		default:
			files = append(files, file)
		}
	}
	source.Files = files
	return source
}

func rulesNeedRouteDat(rules []RoutingRule) (needGeoIP, needGeoSite bool) {
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
			case MatchGeoIP, MatchPrivateIP:
				needGeoIP = true
			case MatchGeoSite:
				needGeoSite = true
			}
		}
	}
	return needGeoIP, needGeoSite
}
