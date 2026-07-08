package privileged

import (
	"path/filepath"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/protocols"
)

const DefaultSocketPath = "/run/veil/helper.sock"

func DefaultPolicy() Policy {
	return Policy{
		StagingRoot:          "/var/lib/veil/staging/generated",
		GeneratedRoot:        "/etc/veil/generated",
		StateRoot:            "/var/lib/veil",
		StatePath:            "/var/lib/veil/state.json",
		KeyPath:              "/etc/veil/state.key",
		BackupPassphrasePath: "/etc/veil/backup.passphrase",
		BackupRoot:           "/var/lib/veil/backups",
		UpdateRoot:           "/var/lib/veil/updates",
		ManagedUnits:         defaultManagedUnits(),
		ManagedUnitPrefixes:  defaultManagedUnitPrefixes(),
		Artifacts: map[string]ArtifactPath{
			"caddy-panel": {
				Staged:    filepath.FromSlash("caddy/panel.Caddyfile"),
				Generated: filepath.FromSlash("caddy/panel.Caddyfile"),
			},
			"hysteria2": {
				Staged:    filepath.FromSlash("hysteria2/server.yaml"),
				Generated: filepath.FromSlash("hysteria2/server.yaml"),
			},
			"mieru": {
				Staged:    filepath.FromSlash("mieru/server_config.json"),
				Generated: filepath.FromSlash("mieru/server_config.json"),
			},
			"olcrtc": {
				Staged:    filepath.FromSlash("olcrtc/server.yaml"),
				Generated: filepath.FromSlash("olcrtc/server.yaml"),
			},
			"warp": {
				Staged:    filepath.FromSlash("sing-box/warp.json"),
				Generated: filepath.FromSlash("sing-box/warp.json"),
			},
		},
		UpdateArtifacts: map[string]string{
			"veil-update": "veil-update.tar.gz",
		},
		FirewallRules: map[string]struct{}{},
	}
}

func defaultManagedUnits() map[string]struct{} {
	units := map[string]struct{}{
		"veil.service":      {},
		"veil-warp.service": {},
	}
	registry := protocols.NewRegistry()
	for _, plugin := range registry.All() {
		rp, ok := protocols.AsRuntimeProvider(plugin)
		if !ok {
			continue
		}
		for _, runtime := range rp.RuntimeDescriptors(nil) {
			if runtime.Unit != "" {
				units[runtime.Unit] = struct{}{}
			}
			if runtime.TemplateUnit != "" {
				units[runtime.TemplateUnit] = struct{}{}
			}
		}
	}
	return units
}

func defaultManagedUnitPrefixes() []string {
	prefixes := []string{}
	seen := map[string]bool{}
	registry := protocols.NewRegistry()
	for _, plugin := range registry.All() {
		rp, ok := protocols.AsRuntimeProvider(plugin)
		if !ok {
			continue
		}
		for _, runtime := range rp.RuntimeDescriptors(nil) {
			for _, unit := range []string{runtime.TemplateUnit, runtime.Unit} {
				prefix, ok := defaultManagedUnitPrefix(unit)
				if !ok || seen[prefix] {
					continue
				}
				seen[prefix] = true
				prefixes = append(prefixes, prefix)
			}
		}
	}
	return prefixes
}

func defaultManagedUnitPrefix(unit string) (string, bool) {
	if !strings.HasSuffix(unit, ".service") {
		return "", false
	}
	idx := strings.Index(unit, "@")
	if idx == -1 {
		return "", false
	}
	return unit[:idx+1], true
}
