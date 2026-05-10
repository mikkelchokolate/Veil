package generatedconfig

import "testing"

func TestArtifactCatalogOwnsGeneratedAndLivePaths(t *testing.T) {
	catalog := NewArtifactCatalog()
	path, ok := catalog.LivePathForStagedConfig("/etc/veil", "/etc/veil/generated/mieru/server_config.json")
	if !ok || path != "/etc/veil/live/mieru/server_config.json" {
		t.Fatalf("live path = %q ok=%v", path, ok)
	}
	spec, ok := catalog.ValidationSpec("/etc/veil/generated/sing-box/warp.json")
	if !ok || spec.Name != "warp" || len(spec.Command) != 4 || spec.Command[0] != "sing-box" {
		t.Fatalf("validation spec = %+v ok=%v", spec, ok)
	}
}

func TestArtifactSpecDerivesStablePlanGeneratedAndLivePaths(t *testing.T) {
	artifact := ArtifactSpec{Subpath: MieruConfigSubpath}
	if got := artifact.PlanPath(); got != "/etc/veil/generated/mieru/server_config.json" {
		t.Fatalf("PlanPath() = %q", got)
	}
	if got := artifact.GeneratedPath("/apply"); got != "/apply/generated/mieru/server_config.json" {
		t.Fatalf("GeneratedPath() = %q", got)
	}
	if got := artifact.LivePath("/apply"); got != "/apply/live/mieru/server_config.json" {
		t.Fatalf("LivePath() = %q", got)
	}
	if !artifact.MatchesGeneratedPath("/tmp/root/generated/mieru/server_config.json") {
		t.Fatalf("artifact should match generated path suffix")
	}
}

func TestArtifactCatalogMatchesValidationAndPromotionSpecs(t *testing.T) {
	catalog := NewArtifactCatalog()
	validation, ok := catalog.ValidationSpec("/apply/generated/hysteria2/server.yaml")
	if !ok {
		t.Fatal("missing validation spec for Hysteria2 generated config")
	}
	if validation.Name != "hysteria2" || validation.Config != "/apply/generated/hysteria2/server.yaml" {
		t.Fatalf("validation spec = %+v", validation)
	}
	if got := validation.Command; len(got) != 5 || got[0] != "hysteria" || got[4] != "--check" {
		t.Fatalf("validation command = %+v", got)
	}
	livePath, ok := catalog.LivePathForStagedConfig("/apply", "/apply/generated/hysteria2/server.yaml")
	if !ok || livePath != "/apply/live/hysteria2/server.yaml" {
		t.Fatalf("live path = %q %v", livePath, ok)
	}
}
