package api

import "path/filepath"

type GeneratedConfigPaths struct {
	ApplyRoot string
}

func NewGeneratedConfigPaths(applyRoot string) GeneratedConfigPaths {
	return GeneratedConfigPaths{ApplyRoot: applyRoot}
}

func (p GeneratedConfigPaths) Generated(subpath string) string {
	return filepath.Join(p.ApplyRoot, "generated", filepath.FromSlash(subpath))
}

func (p GeneratedConfigPaths) Caddyfile() string {
	return p.Generated(generatedCaddyfileSubpath)
}

func (p GeneratedConfigPaths) Hysteria2() string {
	return p.Generated(generatedHysteria2ConfigSubpath)
}

func (p GeneratedConfigPaths) Mieru() string {
	return p.Generated(generatedMieruConfigSubpath)
}

func (p GeneratedConfigPaths) Warp() string {
	return p.Generated(generatedWarpConfigSubpath)
}
