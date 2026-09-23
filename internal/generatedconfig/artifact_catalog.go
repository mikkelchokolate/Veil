package generatedconfig

import (
	pathpkg "path"
	"path/filepath"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/hostenv"
)

const (
	CaddyfileSubpath       = "caddy/config.json"
	CaddyJSONConfigSubpath = "caddy/config.json"
	// Hysteria2 and olcRTC render one config per enabled inbound
	// (hysteria2/<name>.yaml, olcrtc/<name>.yaml); the aggregate server.yaml
	// artifacts are gone (#780). Their catalog subpaths are therefore glob
	// patterns — matching/promotion treat them as patterns, and PlanPath
	// returns "" because no single file represents them.
	Hysteria2ConfigSubpath = "hysteria2/*.yaml"
	MieruConfigSubpath     = "mieru/server_config.json"
	WarpConfigSubpath      = "sing-box/warp.json"
	OlcrtcConfigSubpath    = "olcrtc/*.yaml"
)

type ValidationSpec struct {
	Name    string
	Config  string
	Command []string
}

// ArtifactSpec is the Generated config set Module that owns the path,
// validation, and promotion identity for one generated config artifact.
type ArtifactSpec struct {
	Subpath           string
	ValidationName    string
	ValidationCommand func(string) []string
}

type GeneratedConfigArtifactSpec = ArtifactSpec

func (s ArtifactSpec) PlanPath() string {
	return s.PlanPathForLiveRoot("")
}

// PlanPathForLiveRoot renders the displayed plan path under the actual live
// generated root, so a custom --live-root/--etc-dir install previews the same
// destination the apply job will promote to (issue #636). An empty liveRoot
// falls back to the configured etc dir's generated tree. Glob subpaths
// (per-inbound artifacts) have no single plan path and return "".
func (s ArtifactSpec) PlanPathForLiveRoot(liveRoot string) string {
	if s.Subpath == "" || strings.Contains(s.Subpath, "*") {
		return ""
	}
	root := liveRoot
	if root == "" {
		root = filepath.Join(hostenv.EtcDir(), "generated")
	}
	return filepath.ToSlash(filepath.Join(root, filepath.FromSlash(s.Subpath)))
}

func (s ArtifactSpec) GeneratedPath(applyRoot string) string {
	if s.Subpath == "" {
		return ""
	}
	return filepath.Join(applyRoot, "generated", filepath.FromSlash(s.Subpath))
}

func (s ArtifactSpec) LivePath(applyRoot string) string {
	if s.Subpath == "" {
		return ""
	}
	return filepath.Join(applyRoot, "live", filepath.FromSlash(s.Subpath))
}

func (s ArtifactSpec) ValidationSuffix() string {
	if s.Subpath == "" {
		return ""
	}
	return "/generated/" + filepath.ToSlash(s.Subpath)
}

// MatchesGeneratedPath reports whether path is the artifact's staged file.
// Fixed subpaths must match the generated-relative path exactly — a stray
// caddy/Caddyfile or sibling junk file must not be treated as the managed
// caddy/config.json (#855). Glob subpaths (per-inbound artifacts) match the
// glob against the generated-relative path.
func (s ArtifactSpec) MatchesGeneratedPath(path string) bool {
	sub := filepath.ToSlash(s.Subpath)
	if sub == "" {
		return false
	}
	slashPath := filepath.ToSlash(path)
	idx := strings.Index(slashPath, "/generated/")
	if idx < 0 {
		return false
	}
	rel := slashPath[idx+len("/generated/"):]
	if rel == "" {
		return false
	}
	if strings.Contains(sub, "*") {
		matched, err := pathpkg.Match(sub, rel)
		return err == nil && matched
	}
	return rel == sub
}

func (s ArtifactSpec) ValidationSpec(path string) (ValidationSpec, bool) {
	if s.ValidationName == "" || s.ValidationCommand == nil {
		return ValidationSpec{}, false
	}
	return ValidationSpec{Name: s.ValidationName, Config: path, Command: s.ValidationCommand(path)}, true
}

type ArtifactCatalog struct {
	artifacts []ArtifactSpec
}

type GeneratedConfigArtifactCatalog = ArtifactCatalog

func NewArtifactCatalog(artifacts []ArtifactSpec) ArtifactCatalog {
	out := make([]ArtifactSpec, len(artifacts))
	copy(out, artifacts)
	return ArtifactCatalog{artifacts: out}
}

// NewDefaultArtifactCatalog returns the legacy fixed artifact catalog used by
// tests and generated-config-internal helpers. Production code should build the
// catalog from the protocol registry so new protocol plugins are picked up
// automatically.
func NewDefaultArtifactCatalog() ArtifactCatalog {
	return NewArtifactCatalog([]ArtifactSpec{
		{Subpath: CaddyfileSubpath, ValidationName: "caddy", ValidationCommand: func(path string) []string { return []string{"caddy", "validate", "--config", path} }},
		// Hysteria2 (hysteria), Mieru (mita) and olcRTC have no standalone config
		// check command, so they get no pre-stage syntax validation; a bad config
		// is caught by the post-restart service health check, which rolls back.
		{Subpath: Hysteria2ConfigSubpath, ValidationName: "hysteria2"},
		{Subpath: MieruConfigSubpath, ValidationName: "mieru"},
		{Subpath: WarpConfigSubpath, ValidationName: "warp", ValidationCommand: func(path string) []string { return []string{"sing-box", "check", "-c", path} }},
		{Subpath: OlcrtcConfigSubpath, ValidationName: "olcrtc"},
	})
}

// NewArtifactCatalogFromRegistry builds an artifact catalog from the protocol
// registry. Protocol plugins contribute their own ArtifactSpec, and WARP is
// added explicitly because it is a generated config artifact but not a protocol.
func NewArtifactCatalogFromRegistry(registry ProtocolRegistry) ArtifactCatalog {
	artifacts := append([]ArtifactSpec(nil), registry.ArtifactSpecs()...)
	artifacts = append(artifacts, ArtifactSpec{
		Subpath:        WarpConfigSubpath,
		ValidationName: "warp",
		ValidationCommand: func(path string) []string {
			return []string{"sing-box", "check", "-c", path}
		},
	})
	return NewArtifactCatalog(artifacts)
}

func NewGeneratedConfigArtifactCatalog(artifacts []ArtifactSpec) GeneratedConfigArtifactCatalog {
	return NewArtifactCatalog(artifacts)
}

func (c ArtifactCatalog) All() []ArtifactSpec {
	out := make([]ArtifactSpec, len(c.artifacts))
	copy(out, c.artifacts)
	return out
}

func (c ArtifactCatalog) ValidationSpec(path string) (ValidationSpec, bool) {
	for _, artifact := range c.artifacts {
		if !artifact.MatchesGeneratedPath(path) {
			continue
		}
		return artifact.ValidationSpec(path)
	}
	return ValidationSpec{}, false
}

func (c ArtifactCatalog) LivePathForStagedConfig(applyRoot string, stagedPath string) (string, bool) {
	slashPath := filepath.ToSlash(stagedPath)
	slashRoot := strings.TrimRight(filepath.ToSlash(applyRoot), "/")
	prefix := slashRoot + "/generated/"
	if !strings.HasPrefix(slashPath, prefix) {
		return "", false
	}
	rel := strings.TrimPrefix(slashPath, prefix)
	matched := false
	for _, artifact := range c.artifacts {
		sub := filepath.ToSlash(artifact.Subpath)
		if sub == "" {
			continue
		}
		if strings.Contains(sub, "*") {
			if ok, err := pathpkg.Match(sub, rel); err == nil && ok {
				matched = true
				break
			}
			continue
		}
		if rel == sub {
			matched = true
			break
		}
	}
	if !matched {
		return "", false
	}
	return filepath.Join(applyRoot, "live", filepath.FromSlash(rel)), true
}
