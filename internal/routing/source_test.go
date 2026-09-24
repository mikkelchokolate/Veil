package routing

import "testing"

func sourceFileNames(source RoutingSource) map[string]bool {
	names := map[string]bool{}
	for _, file := range source.Files {
		names[file.Name] = true
	}
	return names
}

func TestEnsureDatSourceFillsGeositeOnlyForGeositeRule(t *testing.T) {
	source := EnsureDatSource(RoutingSource{}, []RoutingRule{{
		Name: "dir", Match: `geosite:category-gov-ru,regexp:.*\.ru$,regexp:.*\.su$`,
		Outbound: "direct", Enabled: true,
	}})
	// Only the geosite dat is pulled: the rule has no geoip matcher, and the
	// ~74MiB geoip.dat would be dead weight (#764 is symmetric for geoip).
	names := sourceFileNames(source)
	if len(source.Files) != 1 || !names["geosite.dat"] {
		t.Fatalf("source files = %+v, want only geosite.dat", source.Files)
	}
}

func TestEnsureDatSourceFillsGeoipOnlyForGeoipRule(t *testing.T) {
	source := EnsureDatSource(RoutingSource{}, []RoutingRule{{
		Name: "dir", Match: "geoip:ru", Outbound: "direct", Enabled: true,
	}})
	names := sourceFileNames(source)
	if len(source.Files) != 1 || !names["geoip.dat"] {
		t.Fatalf("source files = %+v, want only geoip.dat", source.Files)
	}
}

func TestEnsureDatSourceFillsBothForMixedGeoRule(t *testing.T) {
	source := EnsureDatSource(RoutingSource{}, []RoutingRule{{
		Name: "dir", Match: "geoip:ru,geosite:category-ru", Outbound: "direct", Enabled: true,
	}})
	names := sourceFileNames(source)
	if len(source.Files) != 2 || !names["geoip.dat"] || !names["geosite.dat"] {
		t.Fatalf("source files = %+v, want geoip.dat and geosite.dat", source.Files)
	}
}

func TestEnsureDatSourceKeepsExistingFiles(t *testing.T) {
	current := RoutingSource{Files: []RoutingSourceFile{{Name: "custom.dat"}}}
	got := EnsureDatSource(current, []RoutingRule{{
		Name: "dir", Match: "geosite:category-gov-ru", Outbound: "direct", Enabled: true,
	}})
	if len(got.Files) != 1 || got.Files[0].Name != "custom.dat" {
		t.Fatalf("existing source was replaced: %+v", got)
	}
}

func TestEnsureDatSourceSkipsWhenNoGeoMatchers(t *testing.T) {
	got := EnsureDatSource(RoutingSource{}, []RoutingRule{{
		Name: "r", Match: "domain:example.com", Outbound: "direct", Enabled: true,
	}})
	if len(got.Files) != 0 {
		t.Fatalf("unexpected source: %+v", got)
	}
}

func TestEnsureDatSourceFillsGeoipOnlyForPrivateIPOnly(t *testing.T) {
	// geoip:private resolves through the geoip database (the renderer drops
	// geoip: atoms when geoip.dat is absent), so a private-only rule must
	// provision geoip.dat — but never the much larger geosite.dat (#764).
	got := EnsureDatSource(RoutingSource{}, []RoutingRule{{
		Name: "default-direct", Match: "geoip:private", Outbound: "direct", Enabled: true,
	}})
	names := sourceFileNames(got)
	if len(got.Files) != 1 || !names["geoip.dat"] {
		t.Fatalf("geoip:private must pull geoip.dat only: %+v", got.Files)
	}
}

func TestEnsureDatSourceSkipsDisabledGeoRules(t *testing.T) {
	got := EnsureDatSource(RoutingSource{}, []RoutingRule{{
		Name: "off", Match: "geosite:category-ru", Outbound: "direct", Enabled: false,
	}})
	if len(got.Files) != 0 {
		t.Fatalf("disabled rules must not pull dat files: %+v", got.Files)
	}
}
