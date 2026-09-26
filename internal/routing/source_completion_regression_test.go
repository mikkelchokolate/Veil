package routing

import "testing"

// TestEnsureDatSourceCompletesPartialDefaultSource covers #995: a source
// first attached for a geoip-only rule set holds only geoip.dat, and a later
// geosite rule must attach the missing geosite.dat instead of being
// silently dropped forever.
func TestEnsureDatSourceCompletesPartialDefaultSource(t *testing.T) {
	partial := RoutingSource{
		Repository: routeDatSource().Repository,
		Files:      []RoutingSourceFile{routeDatSource().Files[0]}, // geoip.dat only
	}
	got := EnsureDatSource(partial, []RoutingRule{{
		Name: "gs", Match: "geosite:category-ru", Outbound: "direct", Enabled: true,
	}})
	names := sourceFileNames(got)
	if len(got.Files) != 2 || !names["geoip.dat"] || !names["geosite.dat"] {
		t.Fatalf("partial source not completed: %+v", got.Files)
	}
	// The pre-existing entry must be preserved byte-for-byte (operator pins
	// are never rewritten), and provenance stays attached.
	if got.Files[0] != partial.Files[0] {
		t.Fatalf("existing geoip.dat entry was rewritten: %+v", got.Files[0])
	}
	if got.Repository != partial.Repository {
		t.Fatalf("repository changed: %q", got.Repository)
	}
}

// TestEnsureDatSourceCompletesPartialGeositeSource is the symmetric case:
// a geosite-only source gains geoip.dat when a geoip rule appears.
func TestEnsureDatSourceCompletesPartialGeositeSource(t *testing.T) {
	partial := RoutingSource{
		Repository: routeDatSource().Repository,
		Files:      []RoutingSourceFile{routeDatSource().Files[1]}, // geosite.dat only
	}
	got := EnsureDatSource(partial, []RoutingRule{{
		Name: "gi", Match: "geoip:ru", Outbound: "direct", Enabled: true,
	}})
	names := sourceFileNames(got)
	if len(got.Files) != 2 || !names["geoip.dat"] || !names["geosite.dat"] {
		t.Fatalf("partial source not completed: %+v", got.Files)
	}
}

// TestEnsureDatSourceLeavesCompleteSourceAlone ensures a source that already
// covers the needed kinds is returned unchanged (no spurious save churn).
func TestEnsureDatSourceLeavesCompleteSourceAlone(t *testing.T) {
	current := routeDatSource()
	got := EnsureDatSource(current, []RoutingRule{{
		Name: "gi", Match: "geoip:ru", Outbound: "direct", Enabled: true,
	}})
	if len(got.Files) != len(current.Files) {
		t.Fatalf("complete source changed: %+v", got.Files)
	}
}
