package api

import "path/filepath"

type PromotedServiceActionCatalog struct {
	applyRoot string
}

func NewPromotedServiceActionCatalog(applyRoot string) PromotedServiceActionCatalog {
	return PromotedServiceActionCatalog{applyRoot: applyRoot}
}

func (c PromotedServiceActionCatalog) Commands(liveFiles []string) [][]string {
	commands := [][]string{}
	for _, rule := range []struct {
		path    string
		command []string
	}{
		{path: filepath.Join(c.applyRoot, "live", "caddy", "Caddyfile"), command: []string{"systemctl", "reload", "veil-naive.service"}},
		{path: filepath.Join(c.applyRoot, "live", "hysteria2", "server.yaml"), command: []string{"systemctl", "reload", "veil-hysteria2.service"}},
		{path: filepath.Join(c.applyRoot, "live", "sing-box", "warp.json"), command: []string{"systemctl", "reload", "veil-warp.service"}},
		{path: filepath.Join(c.applyRoot, "live", "mieru", "server_config.json"), command: []string{"systemctl", "restart", "veil-mieru.service"}},
	} {
		if containsPath(liveFiles, rule.path) {
			commands = append(commands, append([]string(nil), rule.command...))
		}
	}
	return commands
}
