package generatedconfig

import (
	"path/filepath"
	"testing"
)

func TestArtifactCatalogOwnsGeneratedAndLivePaths(t *testing.T) {
	catalog := NewArtifactCatalog()
	path, ok := catalog.LivePathForStagedConfig(filepath.FromSlash("/etc/veil"), filepath.FromSlash("/etc/veil/generated/mieru/server_config.json"))
	if !ok || path != filepath.FromSlash("/etc/veil/live/mieru/server_config.json") {
		t.Fatalf("live path = %q ok=%v", path, ok)
	}
	spec, ok := catalog.ValidationSpec(filepath.FromSlash("/etc/veil/generated/sing-box/warp.json"))
	if !ok || spec.Name != "warp" || len(spec.Command) != 4 || spec.Command[0] != "sing-box" {
		t.Fatalf("validation spec = %+v ok=%v", spec, ok)
	}
}

func TestArtifactSpecDerivesStablePlanGeneratedAndLivePaths(t *testing.T) {
	artifact := ArtifactSpec{Subpath: MieruConfigSubpath}
	if got := artifact.PlanPath(); got != "/etc/veil/generated/mieru/server_config.json" {
		t.Fatalf("PlanPath() = %q", got)
	}
	if got := artifact.GeneratedPath(filepath.FromSlash("/apply")); got != filepath.FromSlash("/apply/generated/mieru/server_config.json") {
		t.Fatalf("GeneratedPath() = %q", got)
	}
	if got := artifact.LivePath(filepath.FromSlash("/apply")); got != filepath.FromSlash("/apply/live/mieru/server_config.json") {
		t.Fatalf("LivePath() = %q", got)
	}
	if !artifact.MatchesGeneratedPath(filepath.FromSlash("/tmp/root/generated/mieru/server_config.json")) {
		t.Fatalf("artifact should match generated path suffix")
	}
}

func TestArtifactCatalogMatchesValidationAndPromotionSpecs(t *testing.T) {
	catalog := NewArtifactCatalog()
	// caddy has a working standalone validator.
	validation, ok := catalog.ValidationSpec(filepath.FromSlash("/apply/generated/caddy/config.json"))
	if !ok {
		t.Fatal("missing validation spec for caddy generated config")
	}
	if validation.Name != "caddy" || validation.Config != filepath.FromSlash("/apply/generated/caddy/config.json") {
		t.Fatalf("validation spec = %+v", validation)
	}
	if got := validation.Command; len(got) != 6 || got[0] != "caddy" || got[1] != "validate" || got[4] != "--adapter" || got[5] != "json" {
		t.Fatalf("validation command = %+v", got)
	}
	// hysteria2 has no standalone config checker, so it produces no validation spec...
	if _, ok := catalog.ValidationSpec(filepath.FromSlash("/apply/generated/hysteria2/server.yaml")); ok {
		t.Fatal("hysteria2 should have no validation spec (no standalone checker)")
	}
	// ...but its staged config still maps to a live path for promotion.
	livePath, ok := catalog.LivePathForStagedConfig(filepath.FromSlash("/apply"), filepath.FromSlash("/apply/generated/hysteria2/server.yaml"))
	if !ok || livePath != filepath.FromSlash("/apply/live/hysteria2/server.yaml") {
		t.Fatalf("live path = %q %v", livePath, ok)
	}
}
