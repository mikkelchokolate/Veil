package privileged

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/protocols"
)

func pathFromEnv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

const DefaultSocketPath = "/run/veil/helper.sock"

func DefaultPolicy() Policy {
	statePath := pathFromEnv("VEIL_STATE_PATH", "/var/lib/veil/state.json")
	keyPath := pathFromEnv("VEIL_KEY_PATH", "/etc/veil/state.key")
	applyRoot := pathFromEnv("VEIL_APPLY_ROOT", "/var/lib/veil/staging")
	liveRoot := pathFromEnv("VEIL_LIVE_ROOT", "/etc/veil/generated")
	varDir := pathFromEnv("VEIL_VAR_DIR", filepath.Dir(statePath))
	etcDir := pathFromEnv("VEIL_ETC_DIR", filepath.Dir(keyPath))
	return Policy{
		StagingRoot:          filepath.Join(applyRoot, "generated"),
		GeneratedRoot:        liveRoot,
		StateRoot:            varDir,
		StatePath:            statePath,
		KeyPath:              keyPath,
		BackupPassphrasePath: pathFromEnv("VEIL_BACKUP_PASSPHRASE", filepath.Join(etcDir, "backup.passphrase")),
		BackupRoot:           pathFromEnv("VEIL_BACKUP_ROOT", filepath.Join(varDir, "backups")),
		UpdateRoot:           filepath.Join(varDir, "updates"),
		FencePath:            filepath.Join(varDir, "transactions", "runtime-fence.json"),
		RequireFence:         true,
		ManagedUnits:         defaultManagedUnits(),
		ManagedUnitPrefixes:  defaultManagedUnitPrefixes(),
		Artifacts: map[string]ArtifactPath{
			"caddy-panel": {
				Staged:    filepath.FromSlash("caddy/config.json"),
				Generated: filepath.FromSlash("caddy/config.json"),
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
		"veil.service":       {},
		"veil-warp.service":  {},
		"veil-caddy.service": {},
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
	// Legacy per-instance Caddy units may still exist on disk; allow the helper
	// to stop/disable them during orphan cleanup and rollback.
	seen["veil-caddy@"] = true
	prefixes = append(prefixes, "veil-caddy@")
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
