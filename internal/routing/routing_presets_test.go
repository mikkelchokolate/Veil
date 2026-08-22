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
