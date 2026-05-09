package api

import (
	"path/filepath"
	"strings"
)

const (
	generatedCaddyfileSubpath       = "caddy/Caddyfile"
	generatedHysteria2ConfigSubpath = "hysteria2/server.yaml"
	generatedMieruConfigSubpath     = "mieru/server_config.json"
	generatedWarpConfigSubpath      = "sing-box/warp.json"
)

// GeneratedConfigArtifactSpec is the Generated config set Module that owns the
// path, validation, and promotion identity for one generated config artifact.
type GeneratedConfigArtifactSpec struct {
	Subpath           string
	ValidationName    string
	ValidationCommand func(string) []string
}

func (s GeneratedConfigArtifactSpec) PlanPath() string {
	if s.Subpath == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Join("/etc/veil", "generated", filepath.FromSlash(s.Subpath)))
}

func (s GeneratedConfigArtifactSpec) GeneratedPath(applyRoot string) string {
	if s.Subpath == "" {
		return ""
	}
	return filepath.Join(applyRoot, "generated", filepath.FromSlash(s.Subpath))
}

func (s GeneratedConfigArtifactSpec) LivePath(applyRoot string) string {
	if s.Subpath == "" {
		return ""
	}
	return filepath.Join(applyRoot, "live", filepath.FromSlash(s.Subpath))
}

func (s GeneratedConfigArtifactSpec) ValidationSuffix() string {
	if s.Subpath == "" {
		return ""
	}
	return "/generated/" + filepath.ToSlash(s.Subpath)
}

func (s GeneratedConfigArtifactSpec) MatchesGeneratedPath(path string) bool {
	suffix := s.ValidationSuffix()
	return suffix != "" && strings.HasSuffix(filepath.ToSlash(path), suffix)
}

func (s GeneratedConfigArtifactSpec) ValidationSpec(path string) (ConfigValidationSpec, bool) {
	if s.ValidationName == "" || s.ValidationCommand == nil {
		return ConfigValidationSpec{}, false
	}
	return ConfigValidationSpec{Name: s.ValidationName, Config: path, Command: s.ValidationCommand(path)}, true
}

type GeneratedConfigArtifactCatalog struct {
	artifacts []GeneratedConfigArtifactSpec
}

func NewGeneratedConfigArtifactCatalog() GeneratedConfigArtifactCatalog {
	artifacts := []GeneratedConfigArtifactSpec{}
	for _, capability := range NewProtocolCapabilityCatalog().All() {
		if capability.GeneratedConfig.Subpath == "" {
			continue
		}
		artifacts = append(artifacts, capability.GeneratedConfig)
	}
	artifacts = append(artifacts, GeneratedConfigArtifactSpec{Subpath: generatedWarpConfigSubpath, ValidationName: "warp", ValidationCommand: func(path string) []string { return []string{"sing-box", "check", "-c", path} }})
	return GeneratedConfigArtifactCatalog{artifacts: artifacts}
}

func (c GeneratedConfigArtifactCatalog) All() []GeneratedConfigArtifactSpec {
	out := make([]GeneratedConfigArtifactSpec, len(c.artifacts))
	copy(out, c.artifacts)
	return out
}

func (c GeneratedConfigArtifactCatalog) ValidationSpec(path string) (ConfigValidationSpec, bool) {
	for _, artifact := range c.artifacts {
		if !artifact.MatchesGeneratedPath(path) {
			continue
		}
		return artifact.ValidationSpec(path)
	}
	return ConfigValidationSpec{}, false
}

func (c GeneratedConfigArtifactCatalog) LivePathForStagedConfig(applyRoot string, stagedPath string) (string, bool) {
	slashPath := filepath.ToSlash(stagedPath)
	slashRoot := strings.TrimRight(filepath.ToSlash(applyRoot), "/")
	prefix := slashRoot + "/generated/"
	if !strings.HasPrefix(slashPath, prefix) {
		return "", false
	}
	rel := strings.TrimPrefix(slashPath, prefix)
	for _, artifact := range c.artifacts {
		if rel == filepath.ToSlash(artifact.Subpath) {
			return artifact.LivePath(applyRoot), true
		}
	}
	return "", false
}
