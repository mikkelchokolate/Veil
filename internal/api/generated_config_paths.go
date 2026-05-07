package api

import "path/filepath"

type GeneratedConfigPaths struct {
	ApplyRoot string
}

func NewGeneratedConfigPaths(applyRoot string) GeneratedConfigPaths {
	return GeneratedConfigPaths{ApplyRoot: applyRoot}
}

func (p GeneratedConfigPaths) Caddyfile() string {
	return filepath.Join(p.ApplyRoot, "generated", "caddy", "Caddyfile")
}

func (p GeneratedConfigPaths) Hysteria2() string {
	return filepath.Join(p.ApplyRoot, "generated", "hysteria2", "server.yaml")
}

func (p GeneratedConfigPaths) Warp() string {
	return filepath.Join(p.ApplyRoot, "generated", "sing-box", "warp.json")
}
