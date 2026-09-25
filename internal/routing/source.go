package routing

// EnsureDatSource attaches the default geoip/geosite material when enabled
// rules need it. A fully custom source — one carrying any file outside the
// default dat set — is operator-managed and returned untouched, so custom
// rules never silently lose their geo matchers (#764). A default or
// partial-default source (empty, or holding only default dat files such as
// a lone geoip.dat attached for an earlier geoip-only rule set) is completed
// with the needed dat kinds that are missing, so the dat kind a later rule
// requires is no longer dropped forever by the first attached kind (#995).
// Only the dat files the enabled matchers actually consume are attached:
// geoip matchers (including geoip:private) pull geoip.dat, geosite matchers
// pull geosite.dat — a private-only rule set must not download the ~74MiB
// geosite.dat it never reads (#764).
func EnsureDatSource(current RoutingSource, rules []RoutingRule) RoutingSource {
	needGeoIP, needGeoSite := rulesNeedRouteDat(rules)
	if !needGeoIP && !needGeoSite {
		return current
	}
	present := make(map[string]bool, len(current.Files))
	for _, file := range current.Files {
		present[file.Name] = true
		if file.Name != "geoip.dat" && file.Name != "geosite.dat" {
			// Any non-default filename marks the source as custom: the
			// operator owns its file list, so never merge default material
			// into it.
			return current
		}
	}
	source := routeDatSource()
	missing := make([]RoutingSourceFile, 0, len(source.Files))
	for _, file := range source.Files {
		if present[file.Name] {
			continue
		}
		switch file.Name {
		case "geoip.dat":
			if needGeoIP {
				missing = append(missing, file)
			}
		case "geosite.dat":
			if needGeoSite {
				missing = append(missing, file)
			}
		default:
			missing = append(missing, file)
		}
	}
	if len(current.Files) == 0 {
		// Fresh attach: adopt the default source wholesale so its repository
		// provenance is recorded with the file list.
		source.Files = missing
		return source
	}
	if len(missing) == 0 {
		return current
	}
	out := current
	if out.Repository == "" {
		out.Repository = source.Repository
	}
	out.Files = append(append([]RoutingSourceFile(nil), current.Files...), missing...)
	return out
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
