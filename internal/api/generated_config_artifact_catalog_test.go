package api

import "testing"

func TestGeneratedConfigArtifactSpecDerivesProtocolPaths(t *testing.T) {
	capability, ok := NewProtocolCapabilityCatalog().ForProtocol("mieru")
	if !ok {
		t.Fatal("missing Mieru capability")
	}
	artifact := capability.GeneratedConfig
	if artifact.Subpath != "mieru/server_config.json" {
		t.Fatalf("Mieru generated config subpath = %q", artifact.Subpath)
	}
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

func TestGeneratedConfigArtifactCatalogMatchesValidationAndPromotionSpecs(t *testing.T) {
	catalog := NewGeneratedConfigArtifactCatalog()
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
