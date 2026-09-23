package routing

import (
	"strings"
	"testing"
)

func TestRouteDatSource(t *testing.T) {
	source := routeDatSource()
	wantRepository := routingRulesRepository + "/releases/tag/" + routingRulesRelease
	if source.Repository != wantRepository {
		t.Fatalf("repository = %q, want %q", source.Repository, wantRepository)
	}
	if len(source.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(source.Files))
	}
	for _, file := range source.Files {
		if file.Name == "" {
			t.Fatalf("file name must not be empty: %+v", file)
		}
		if file.URL == "" {
			t.Fatalf("file URL must not be empty: %+v", file)
		}
		if file.SHA256URL == "" {
			t.Fatalf("file SHA256URL must not be empty: %+v", file)
		}
		if file.PinnedSHA256 == "" {
			t.Fatalf("file pinned SHA-256 must not be empty: %+v", file)
		}
	}
}

func TestRoutingPresetProfiles(t *testing.T) {
	presets := routingPresetProfiles()
	if len(presets) == 0 {
		t.Fatal("expected at least one preset")
	}
	for _, preset := range presets {
		if preset.Name == "" {
			t.Fatalf("preset name must not be empty: %+v", preset)
		}
		if preset.Description == "" {
			t.Fatalf("preset description must not be empty: %+v", preset)
		}
		for _, rule := range preset.Rules {
			if rule.Name == "" || rule.Match == "" || rule.Outbound == "" {
				t.Fatalf("rule must have name, match and outbound: %+v", rule)
			}
		}
	}
}

// TestRoutingPresetRUBlockedMatchers locks the RU-blocked preset to the real
// runetfreedom lists. The previous geosite:geolocation-!ru placeholder
// referenced a SagerNet artifact that was never published, so a matcher-only
// smoke check would green a preset that downloads nothing (#778/#852).
func TestRoutingPresetRUBlockedMatchers(t *testing.T) {
	preset, ok := routingPresetByName("RU-blocked")
	if !ok {
		t.Fatal("RU-blocked preset not found")
	}
	type wantRule struct{ match, outbound string }
	want := []wantRule{
		{"geoip:ru-blocked", "proxy"},
		{"geosite:ru-blocked", "proxy"},
	}
	if len(preset.Rules) != len(want) {
		t.Fatalf("RU-blocked rules = %+v, want %d entries", preset.Rules, len(want))
	}
	for i, w := range want {
		if preset.Rules[i].Match != w.match || preset.Rules[i].Outbound != w.outbound || !preset.Rules[i].Enabled {
			t.Fatalf("RU-blocked rule %d = %+v, want match=%q outbound=%q enabled", i, preset.Rules[i], w.match, w.outbound)
		}
	}
	for _, rule := range preset.Rules {
		if strings.Contains(rule.Match, "geolocation-!ru") || strings.Contains(rule.Match, "geolocation-ru") {
			t.Fatalf("RU-blocked preset must not reference the unpublished geolocation-ru list: %+v", rule)
		}
	}
	if len(preset.Source.Files) == 0 {
		t.Fatal("RU-blocked preset must carry the routing dat source so geoip/geosite matchers resolve")
	}
}

// TestRoutingPresetAllExceptRussiaMatchers locks the privacy baseline rules:
// private IP space direct first, then RU geoip/geosite, then all-through-proxy.
func TestRoutingPresetAllExceptRussiaMatchers(t *testing.T) {
	preset, ok := routingPresetByName("all-except-Russia")
	if !ok {
		t.Fatal("all-except-Russia preset not found")
	}
	wantMatches := []string{"geoip:private", "geoip:ru", "geosite:category-ru", "all"}
	wantOutbounds := []string{"direct", "direct", "direct", "proxy"}
	if len(preset.Rules) != len(wantMatches) {
		t.Fatalf("all-except-Russia rules = %+v, want %d entries", preset.Rules, len(wantMatches))
	}
	for i := range wantMatches {
		if preset.Rules[i].Match != wantMatches[i] || preset.Rules[i].Outbound != wantOutbounds[i] {
			t.Fatalf("all-except-Russia rule %d = %+v, want match=%q outbound=%q", i, preset.Rules[i], wantMatches[i], wantOutbounds[i])
		}
	}
}

func TestRoutingPresetByName(t *testing.T) {
	presets := routingPresetProfiles()
	for _, preset := range presets {
		got, ok := routingPresetByName(preset.Name)
		if !ok {
			t.Fatalf("routingPresetByName(%q) not found", preset.Name)
		}
		if got.Name != preset.Name {
			t.Fatalf("routingPresetByName(%q) = %+v, want %+v", preset.Name, got, preset)
		}
	}

	if _, ok := routingPresetByName("definitely-missing-preset-name"); ok {
		t.Fatal("expected missing preset to not be found")
	}
}

func TestRouteDatSourceGeoSiteDatURL(t *testing.T) {
	source := routeDatSource()
	var geoSiteFound bool
	for _, file := range source.Files {
		if file.Name == "geosite.dat" {
			geoSiteFound = true
			if !strings.Contains(file.URL, "geosite.dat") {
				t.Fatalf("geosite URL does not contain filename: %q", file.URL)
			}
			if !strings.Contains(file.SHA256URL, "geosite.dat") {
				t.Fatalf("geosite SHA256URL does not contain filename: %q", file.SHA256URL)
			}
		}
	}
	if !geoSiteFound {
		t.Fatal("geosite.dat file not found in route dat source")
	}
}
