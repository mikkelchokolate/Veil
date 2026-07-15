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

func NewManagedRuntimeCatalog() ManagedRuntimeCatalog {
	inbounds, warp := loadSnapshotFromState()
	return NewManagedRuntimeCatalogFor(inbounds, warp)
}

func NewManagedRuntimeCatalogFor(inbounds []Inbound, warp WarpConfig) ManagedRuntimeCatalog {
	runtimes := []ManagedRuntime{{Name: "veil", ActionName: "veil", Unit: renderer.UnitVeil, ManualRestart: true}}

	registry := protocols.NewRegistry()
	if len(inbounds) == 0 {
		// For backward compatibility and unit tests that expect a clean,
		// non-configured environment to list the default protocol runtimes.
		for _, p := range registry.All() {
			rp, ok := protocols.AsRuntimeProvider(p)
			if !ok {
				continue
			}
			runtimes = append(runtimes, rp.RuntimeDescriptors(nil)...)
		}
	} else {
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
	}

	// A hysteria2 inbound with a domain needs the consolidated Caddy process
	// to obtain and manage the ACME certificate, even when no naiveproxy
	// inbound exists. The naiveproxy RuntimeDescriptors already adds the Caddy
	// runtime when a naive inbound is present, so only add it here if it is
	// missing and a hysteria2 domain requires it.
	if !hasCaddyRuntime(runtimes) && hasHysteria2Domain(inbounds) {
		runtimes = append(runtimes, caddyManagedRuntime())
	}

	// sing-box / warp
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

func hasCaddyRuntime(runtimes []ManagedRuntime) bool {
	for _, rt := range runtimes {
		if rt.Unit == renderer.UnitCaddy {
			return true
		}
	}
	return false
}

func hasHysteria2Domain(inbounds []Inbound) bool {
	for _, inb := range inbounds {
		if !inb.Enabled || inb.Protocol != "hysteria2" {
			continue
		}
		if inboundDomain(inb) != "" {
			return true
		}
	}
	return false
}

func caddyManagedRuntime() ManagedRuntime {
	return ManagedRuntime{
		Name:             "veil-caddy.service",
		ActionName:       "caddy",
		Unit:             renderer.UnitCaddy,
		TemplateUnit:     renderer.UnitCaddy,
		PromotedSubpath:  generatedconfig.CaddyJSONConfigSubpath,
		PromotedVerb:     "reload",
		ManualRestart:    true,
		HealthCheckAfter: true,
	}
}

func loadSnapshotFromState() ([]Inbound, WarpConfig) {
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
		return nil, WarpConfig{}
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
		Inbounds []Inbound  `json:"inbounds"`
		Warp     WarpConfig `json:"warp"`
	}
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, WarpConfig{}
	}
	return snapshot.Inbounds, snapshot.Warp
}
