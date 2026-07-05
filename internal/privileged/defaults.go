package privileged

import "path/filepath"

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
		ManagedUnits: map[string]struct{}{
			"veil.service":       {},
			"veil-mieru.service": {},
			"veil-warp.service":  {},
			"veil-caddy.service": {},
		},
		ManagedUnitPrefixes: []string{
			"veil-caddy.service",
			"veil-hysteria2@",
			"veil-olcrtc@",
		},
		Artifacts: map[string]ArtifactPath{
			"caddy-panel": {
				Staged:    filepath.FromSlash("caddy/config.json"),
				Generated: filepath.FromSlash("caddy/config.json"),
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
