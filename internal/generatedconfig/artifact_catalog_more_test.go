package generatedconfig

import (
	"path/filepath"
	"testing"
)

func TestArtifactSpecHandlesEmptySubpath(t *testing.T) {
	empty := ArtifactSpec{}
	if got := empty.PlanPath(); got != "" {
		t.Fatalf("PlanPath() = %q, want empty", got)
	}
	if got := empty.GeneratedPath("/apply"); got != "" {
		t.Fatalf("GeneratedPath() = %q, want empty", got)
	}
	if got := empty.LivePath("/apply"); got != "" {
		t.Fatalf("LivePath() = %q, want empty", got)
	}
	if got := empty.ValidationSuffix(); got != "" {
		t.Fatalf("ValidationSuffix() = %q, want empty", got)
	}
}

func TestArtifactSpecValidationSuffix(t *testing.T) {
	spec := ArtifactSpec{Subpath: MieruConfigSubpath}
	if got := spec.ValidationSuffix(); got != "/generated/mieru/server_config.json" {
		t.Fatalf("ValidationSuffix() = %q", got)
	}
}

func TestGeneratedConfigArtifactCatalogAlias(t *testing.T) {
	catalog := NewGeneratedConfigArtifactCatalog([]ArtifactSpec{{Subpath: CaddyfileSubpath}})
	all := catalog.All()
	if len(all) == 0 {
		t.Fatal("expected non-empty artifact catalog")
	}
	found := false
	for _, a := range all {
		if a.Subpath == CaddyfileSubpath {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing caddy artifact in catalog: %+v", all)
	}
}

func TestArtifactCatalogLivePathForStagedConfigRejectsUnknownPaths(t *testing.T) {
	catalog := NewDefaultArtifactCatalog()
	root := filepath.FromSlash("/etc/veil")

	// Path not under /generated/ prefix.
	if _, ok := catalog.LivePathForStagedConfig(root, filepath.FromSlash("/etc/veil/live/mieru/server_config.json")); ok {
		t.Fatal("expected false for non-generated path")
	}

	// Path under /generated/ but not matching a known artifact directory.
	if _, ok := catalog.LivePathForStagedConfig(root, filepath.FromSlash("/etc/veil/generated/unknown/file.txt")); ok {
		t.Fatal("expected false for unknown artifact directory")
	}
}
