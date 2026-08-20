package routing

import "testing"

func TestEnsureDatSourceFillsGeoFilesForGeositeRule(t *testing.T) {
	source := EnsureDatSource(RoutingSource{}, []RoutingRule{{
		Name: "dir", Match: `geosite:category-gov-ru,regexp:.*\.ru$,regexp:.*\.su$`,
		Outbound: "direct", Enabled: true,
	}})
	if len(source.Files) != 2 {
		t.Fatalf("source files = %+v, want geoip.dat and geosite.dat", source.Files)
	}
	names := map[string]bool{}
	for _, file := range source.Files {
		names[file.Name] = true
	}
	if !names["geoip.dat"] || !names["geosite.dat"] {
		t.Fatalf("source files = %+v", source.Files)
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

func TestEnsureDatSourceSkipsPrivateIPOnly(t *testing.T) {
	got := EnsureDatSource(RoutingSource{}, []RoutingRule{{
		Name: "default-direct", Match: "geoip:private", Outbound: "direct", Enabled: true,
	}})
	if len(got.Files) != 0 {
		t.Fatalf("geoip:private must not pull geosite.dat: %+v", got)
	}
}
