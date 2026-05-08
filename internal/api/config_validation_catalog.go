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
	for _, capability := range NewProtocolCapabilityCatalog().All() {
		if strings.HasSuffix(slashPath, capability.GeneratedConfigSuffix) {
			return ConfigValidationSpec{Name: capability.ValidationName, Config: path, Command: capability.ValidationCommand(path)}, true
		}
	}
	for _, rule := range []struct {
		suffix string
		name   string
		cmd    func(string) []string
	}{
		{suffix: "/generated/sing-box/warp.json", name: "warp", cmd: func(path string) []string { return []string{"sing-box", "check", "-c", path} }},
	} {
		if strings.HasSuffix(slashPath, rule.suffix) {
			return ConfigValidationSpec{Name: rule.name, Config: path, Command: rule.cmd(path)}, true
		}
	}
	return ConfigValidationSpec{}, false
}
