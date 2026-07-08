package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/protocols"
	"github.com/mikkelchokolate/Veil/internal/renderer"
	"github.com/mikkelchokolate/Veil/internal/secrets"
	"github.com/mikkelchokolate/Veil/internal/service"
)

type ManagedRuntime = service.ManagedRuntime
type ManagedRuntimeCatalog = service.ManagedRuntimeCatalog

// NewManagedRuntimeCatalog returns the broad management catalog used for apply,
// repair, privileged policy checks, and backward-compatible service commands. It
// intentionally includes fallback/template runtimes so first-apply, recovery,
// orphan cleanup, and rollback paths can validate, promote, stop, and restart
// managed units even when the current saved state no longer references them.
func NewManagedRuntimeCatalog() ManagedRuntimeCatalog {
	return NewManagedRuntimeCatalogFor(nil, WarpConfig{})
}

// NewManagedRuntimeCatalogFor returns a state-scoped catalog for active apply
// planning. Pass nil inbounds for the broad fallback/template catalog.
func NewManagedRuntimeCatalogFor(inbounds []Inbound, warp WarpConfig) ManagedRuntimeCatalog {
	runtimes := []ManagedRuntime{{Name: "veil", ActionName: "veil", Unit: renderer.UnitVeil, ManualRestart: true}}

	registry := protocols.NewRegistry()
	for _, p := range registry.All() {
		rp, ok := protocols.AsRuntimeProvider(p)
		if !ok {
			continue
		}
		selected := enabledInboundsForProtocol(inbounds, p.Protocol())
		if len(inbounds) == 0 {
			runtimes = append(runtimes, rp.RuntimeDescriptors(nil)...)
			continue
		}
		if len(selected) > 0 {
			runtimes = append(runtimes, rp.RuntimeDescriptors(selected)...)
		}
	}

	if warp.Enabled || len(inbounds) == 0 {
		runtimes = append(runtimes, ManagedRuntime{
			Name:            "sing-box",
			ActionName:      "sing-box",
			Unit:            renderer.UnitWarp,
			PromotedSubpath: generatedconfig.WarpConfigSubpath,
			// restart, not reload: on first enable the unit is inactive (reload
			// fails), and the warp unit's ExecReload only validates — restart is
			// what actually applies a new WARP config.
			PromotedVerb:     "restart",
			ManualRestart:    true,
			HealthCheckAfter: true,
		})
	}

	return sortManagedRuntimes(runtimes)
}

// NewVisibleManagedRuntimeCatalog returns the operator-facing catalog for the
// dashboard and /api/status. Once a saved state exists, it only exposes services
// configured in that state, plus Veil itself. If state is absent, it falls back to
// the broad catalog so first-run/recovery environments retain their legacy
// management affordances.
func NewVisibleManagedRuntimeCatalog() ManagedRuntimeCatalog {
	settings, inbounds, warp, ok := loadSnapshotFromStateWithOK()
	if !ok {
		return NewManagedRuntimeCatalogFor(inbounds, warp)
	}
	return NewManagedRuntimeCatalogForSnapshot(settings, inbounds, warp)
}

func NewManagedRuntimeCatalogForSnapshot(settings Settings, inbounds []Inbound, warp WarpConfig) ManagedRuntimeCatalog {
	runtimes := []ManagedRuntime{{Name: "veil", ActionName: "veil", Unit: renderer.UnitVeil, ManualRestart: true}}

	if settings.PanelAccess == "caddy" {
		runtimes = append(runtimes, ManagedRuntime{
			Name:             "caddy-panel",
			ActionName:       "caddy-panel",
			Protocol:         "naiveproxy",
			Transport:        "tcp",
			Unit:             "veil-caddy@panel.service",
			TemplateUnit:     renderer.UnitCaddy,
			PromotedSubpath:  generatedconfig.CaddyfileSubpath,
			PromotedVerb:     "restart",
			ManualRestart:    true,
			HealthCheckAfter: true,
		})
	}

	registry := protocols.NewRegistry()
	for _, p := range registry.All() {
		rp, ok := protocols.AsRuntimeProvider(p)
		if !ok {
			continue
		}
		selected := enabledInboundsForProtocol(inbounds, p.Protocol())
		if len(selected) > 0 {
			runtimes = append(runtimes, rp.RuntimeDescriptors(selected)...)
		}
	}

	if warp.Enabled {
		runtimes = append(runtimes, ManagedRuntime{
			Name:             "sing-box",
			ActionName:       "sing-box",
			Unit:             renderer.UnitWarp,
			PromotedSubpath:  generatedconfig.WarpConfigSubpath,
			PromotedVerb:     "restart",
			ManualRestart:    true,
			HealthCheckAfter: true,
		})
	}

	return sortManagedRuntimes(runtimes)
}

func sortManagedRuntimes(runtimes []ManagedRuntime) ManagedRuntimeCatalog {
	sort.SliceStable(runtimes, func(i, j int) bool {
		if runtimes[i].Name == "veil" {
			return true
		}
		if runtimes[j].Name == "veil" {
			return false
		}
		return runtimes[i].Name < runtimes[j].Name
	})
	return service.NewManagedRuntimeCatalog(runtimes)
}

func enabledInboundsForProtocol(inbounds []Inbound, protocol string) []Inbound {
	selected := make([]Inbound, 0, len(inbounds))
	for _, inbound := range inbounds {
		if inbound.Enabled && inbound.Protocol == protocol {
			selected = append(selected, inbound)
		}
	}
	return selected
}

func loadSnapshotFromStateWithOK() (Settings, []Inbound, WarpConfig, bool) {
	statePath := strings.TrimSpace(os.Getenv("VEIL_STATE_PATH"))
	if statePath == "" {
		if runtime.GOOS == "windows" {
			pd := os.Getenv("ProgramData")
			if pd == "" {
				pd = `C:\ProgramData`
			}
			statePath = filepath.Join(pd, "Veil", "state.json")
		} else {
			statePath = "/var/lib/veil/state.json"
		}
	}
	keyPath := strings.TrimSpace(os.Getenv("VEIL_KEY_PATH"))
	if keyPath == "" {
		if runtime.GOOS == "windows" {
			pd := os.Getenv("ProgramData")
			if pd == "" {
				pd = `C:\ProgramData`
			}
			keyPath = filepath.Join(pd, "Veil", "state.key")
		} else {
			keyPath = "/etc/veil/state.key"
		}
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		return Settings{}, nil, WarpConfig{}, false
	}
	if keyPath != "" {
		if key, keyErr := secrets.LoadOrCreateKey(keyPath); keyErr == nil {
			if ciph, ciphErr := secrets.NewCipher(*key); ciphErr == nil {
				if decrypted, decErr := ciph.Decrypt(string(data)); decErr == nil {
					data = []byte(decrypted)
				}
			}
		}
	}
	var snapshot struct {
		Settings Settings   `json:"settings"`
		Inbounds []Inbound  `json:"inbounds"`
		Warp     WarpConfig `json:"warp"`
	}
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return Settings{}, nil, WarpConfig{}, false
	}
	return snapshot.Settings, snapshot.Inbounds, snapshot.Warp, true
}
