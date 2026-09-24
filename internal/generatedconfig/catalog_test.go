package generatedconfig

import (
	"path/filepath"
	"testing"
)

func TestArtifactCatalogOwnsGeneratedAndLivePaths(t *testing.T) {
	catalog := NewDefaultArtifactCatalog()
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
	// PlanPath() with no explicit root resolves hostenv.EtcDir(); pin the
	// packaged default by clearing the env overrides so the assertion stays
	// hermetic when VEIL_ETC_DIR/VEIL_LIVE_ROOT leak into the test process.
	for _, key := range []string{"VEIL_ETC_DIR", "VEIL_LIVE_ROOT", "VEIL_KEY_PATH"} {
		t.Setenv(key, "")
	}
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
	// Matching is exact against the generated-relative path: sibling files and
	// wrong basenames must not be treated as the managed artifact (#855).
	for _, wrong := range []string{
		"/tmp/root/generated/mieru/other_config.json",
		"/tmp/root/generated/mieru/server_config.json.bak",
		"/tmp/root/generated/mieru/extra/server_config.json",
		"/tmp/root/generated/mieru",
	} {
		if artifact.MatchesGeneratedPath(filepath.FromSlash(wrong)) {
			t.Fatalf("artifact matched wrong basename %q", wrong)
		}
	}
	// Glob subpaths match any file of the right extension in the artifact dir.
	perInbound := ArtifactSpec{Subpath: Hysteria2ConfigSubpath}
	if !perInbound.MatchesGeneratedPath(filepath.FromSlash("/tmp/root/generated/hysteria2/edge.yaml")) {
		t.Fatalf("per-inbound artifact should match hysteria2/edge.yaml")
	}
	for _, wrong := range []string{
		"/tmp/root/generated/hysteria2/edge.txt",
		"/tmp/root/generated/hysteria2/nested/edge.yaml",
		"/tmp/root/generated/other/edge.yaml",
	} {
		if perInbound.MatchesGeneratedPath(filepath.FromSlash(wrong)) {
			t.Fatalf("per-inbound artifact matched %q", wrong)
		}
	}
}

func TestArtifactCatalogMatchesValidationAndPromotionSpecs(t *testing.T) {
	catalog := NewDefaultArtifactCatalog()
	// caddy has a working standalone validator; the managed artifact is the
	// consolidated JSON config, not a legacy Caddyfile (#855).
	validation, ok := catalog.ValidationSpec(filepath.FromSlash("/apply/generated/caddy/config.json"))
	if !ok {
		t.Fatal("missing validation spec for caddy generated config")
	}
	if validation.Name != "caddy" || validation.Config != filepath.FromSlash("/apply/generated/caddy/config.json") {
		t.Fatalf("validation spec = %+v", validation)
	}
	if got := validation.Command; len(got) != 4 || got[0] != "caddy" || got[1] != "validate" {
		t.Fatalf("validation command = %+v", got)
	}
	// A staged legacy Caddyfile is not the managed artifact and gets no spec.
	if _, ok := catalog.ValidationSpec(filepath.FromSlash("/apply/generated/caddy/panel.Caddyfile")); ok {
		t.Fatal("legacy Caddyfile should not match the caddy validation spec")
	}
	// hysteria2 has no standalone config checker, so it produces no validation spec...
	if _, ok := catalog.ValidationSpec(filepath.FromSlash("/apply/generated/hysteria2/edge.yaml")); ok {
		t.Fatal("hysteria2 should have no validation spec (no standalone checker)")
	}
	// ...but its staged per-inbound config still maps to a live path for promotion.
	livePath, ok := catalog.LivePathForStagedConfig(filepath.FromSlash("/apply"), filepath.FromSlash("/apply/generated/hysteria2/edge.yaml"))
	if !ok || livePath != filepath.FromSlash("/apply/live/hysteria2/edge.yaml") {
		t.Fatalf("live path = %q %v", livePath, ok)
	}
}
