package generatedconfig

import (
	"path/filepath"
	"strings"
)

const (
	CaddyJSONConfigSubpath = "caddy/config.json"
	Hysteria2ConfigSubpath = "hysteria2/server.yaml"
	MieruConfigSubpath     = "mieru/server_config.json"
	WarpConfigSubpath      = "sing-box/warp.json"
	OlcrtcConfigSubpath    = "olcrtc/server.yaml"
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
	if s.Subpath == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Join("/etc/veil", "generated", filepath.FromSlash(s.Subpath)))
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

func (s ArtifactSpec) MatchesGeneratedPath(path string) bool {
	slashPath := filepath.ToSlash(path)
	dir := s.Subpath
	if idx := strings.Index(dir, "/"); idx != -1 {
		dir = dir[:idx]
	}
	return dir != "" && strings.Contains(slashPath, "/generated/"+dir+"/")
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

func NewArtifactCatalog() ArtifactCatalog {
	return ArtifactCatalog{artifacts: []ArtifactSpec{
		{Subpath: CaddyJSONConfigSubpath, ValidationName: "caddy", ValidationCommand: func(path string) []string { return []string{"caddy", "validate", "--config", path} }},
		// Hysteria2 (hysteria), Mieru (mita) and olcRTC have no standalone config
		// check command, so they get no pre-stage syntax validation; a bad config
		// is caught by the post-restart service health check, which rolls back.
		{Subpath: Hysteria2ConfigSubpath, ValidationName: "hysteria2"},
		{Subpath: MieruConfigSubpath, ValidationName: "mieru"},
		{Subpath: WarpConfigSubpath, ValidationName: "warp", ValidationCommand: func(path string) []string { return []string{"sing-box", "check", "-c", path} }},
		{Subpath: OlcrtcConfigSubpath, ValidationName: "olcrtc"},
	}}
}

func NewGeneratedConfigArtifactCatalog() GeneratedConfigArtifactCatalog { return NewArtifactCatalog() }

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
		dir := artifact.Subpath
		if idx := strings.Index(dir, "/"); idx != -1 {
			dir = dir[:idx]
		}
		if strings.HasPrefix(filepath.ToSlash(rel), dir+"/") {
			matched = true
			break
		}
	}
	if !matched {
		return "", false
	}
	return filepath.Join(applyRoot, "live", filepath.FromSlash(rel)), true
}
