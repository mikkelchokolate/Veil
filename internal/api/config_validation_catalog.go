package api

import (
	"path/filepath"
	"strings"
)

type ConfigValidationSpec struct {
	Name    string
	Config  string
	Command []string
}

type ConfigValidationCatalog struct{}

func NewConfigValidationCatalog() ConfigValidationCatalog { return ConfigValidationCatalog{} }

func (ConfigValidationCatalog) Match(path string) (ConfigValidationSpec, bool) {
	slashPath := filepath.ToSlash(path)
	for _, rule := range []struct {
		suffix string
		name   string
		cmd    func(string) []string
	}{
		{suffix: "/generated/caddy/Caddyfile", name: "caddy", cmd: func(path string) []string { return []string{"caddy", "validate", "--config", path} }},
		{suffix: "/generated/hysteria2/server.yaml", name: "hysteria2", cmd: func(path string) []string { return []string{"hysteria", "server", "--config", path, "--check"} }},
		{suffix: "/generated/sing-box/warp.json", name: "warp", cmd: func(path string) []string { return []string{"sing-box", "check", "-c", path} }},
		{suffix: "/generated/mieru/server_config.json", name: "mieru", cmd: func(path string) []string { return []string{"mieru", "check", "-c", path} }},
	} {
		if strings.HasSuffix(slashPath, rule.suffix) {
			return ConfigValidationSpec{Name: rule.name, Config: path, Command: rule.cmd(path)}, true
		}
	}
	return ConfigValidationSpec{}, false
}
