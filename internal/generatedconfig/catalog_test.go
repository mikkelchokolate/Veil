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
