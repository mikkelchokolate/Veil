package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
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

	hasNaive := false
	hasMieru := false
	hasHysteria := false
	hasOlcrtc := false

	for _, inbound := range inbounds {
		if inbound.Enabled {
			if inbound.Protocol == "naiveproxy" {
				hasNaive = true
			}
			if inbound.Protocol == "mieru" {
				hasMieru = true
			}
			if inbound.Protocol == "hysteria2" {
				hasHysteria = true
			}
			if inbound.Protocol == "olcrtc" {
				hasOlcrtc = true
			}
		}
	}

	// For backward compatibility and unit tests that expect a clean, non-configured environment
	// to list the default runtimes:
	if len(inbounds) == 0 {
		hasNaive = true
		hasMieru = true
		hasHysteria = true
		hasOlcrtc = true
	}

	// 1. naiveproxy (template-unit per inbound)
	if hasNaive {
		naiveCount := 0
		for _, inbound := range inbounds {
			if inbound.Enabled && inbound.Protocol == "naiveproxy" {
				naiveCount++
				runtimes = append(runtimes, ManagedRuntime{
					Name:             "caddy-" + inbound.Name,
					ActionName:       "caddy-" + inbound.Name,
					Protocol:         "naiveproxy",
					Transport:        "tcp",
					Unit:             "veil-caddy@" + inbound.Name + ".service",
					PromotedSubpath:  "caddy/" + inbound.Name + ".Caddyfile",
					PromotedVerb:     "reload",
					ManualRestart:    true,
					HealthCheckAfter: true,
				})
			}
		}
		if naiveCount == 0 {
			runtimes = append(runtimes, ManagedRuntime{
				Name:             "caddy-panel",
				ActionName:       "caddy-panel",
				Protocol:         "naiveproxy",
				Transport:        "tcp",
				Unit:             "veil-caddy@panel.service",
				PromotedSubpath:  "caddy/panel.Caddyfile",
				PromotedVerb:     "reload",
				ManualRestart:    true,
				HealthCheckAfter: true,
			})
		}
	}

	// 2. hysteria2 (template-unit per inbound)
	if hasHysteria {
		hysteriaCount := 0
		for _, inbound := range inbounds {
			if inbound.Enabled && inbound.Protocol == "hysteria2" {
				hysteriaCount++
				runtimes = append(runtimes, ManagedRuntime{
					Name:             "hysteria2-" + inbound.Name,
					ActionName:       "hysteria2-" + inbound.Name,
					Protocol:         "hysteria2",
					Transport:        "udp",
					Unit:             "veil-hysteria2@" + inbound.Name + ".service",
					PromotedSubpath:  "hysteria2/" + inbound.Name + ".yaml",
					PromotedVerb:     "reload",
					ManualRestart:    true,
					HealthCheckAfter: true,
				})
			}
		}
		if hysteriaCount == 0 {
			runtimes = append(runtimes, ManagedRuntime{
				Name:             "hysteria2",
				ActionName:       "hysteria2",
				Protocol:         "hysteria2",
				Transport:        "udp",
				Unit:             renderer.UnitHysteria2,
				PromotedSubpath:  generatedconfig.Hysteria2ConfigSubpath,
				PromotedVerb:     "reload",
				ManualRestart:    true,
				HealthCheckAfter: true,
			})
		}
	}

	// 3. olcrtc (template-unit per inbound)
	if hasOlcrtc {
		olcrtcCount := 0
		for _, inbound := range inbounds {
			if inbound.Enabled && inbound.Protocol == "olcrtc" {
				olcrtcCount++
				runtimes = append(runtimes, ManagedRuntime{
					Name:             "olcrtc-" + inbound.Name,
					ActionName:       "olcrtc-" + inbound.Name,
					Protocol:         "olcrtc",
					Transport:        "udp",
					Unit:             "veil-olcrtc@" + inbound.Name + ".service",
					PromotedSubpath:  "olcrtc/" + inbound.Name + ".yaml",
					PromotedVerb:     "restart",
					ManualRestart:    true,
					HealthCheckAfter: true,
				})
			}
		}
		if olcrtcCount == 0 {
			runtimes = append(runtimes, ManagedRuntime{
				Name:             "olcrtc",
				ActionName:       "olcrtc",
				Protocol:         "olcrtc",
				Transport:        "udp",
				Unit:             renderer.UnitOlcrtc,
				PromotedSubpath:  generatedconfig.OlcrtcConfigSubpath,
				PromotedVerb:     "restart",
				ManualRestart:    true,
				HealthCheckAfter: true,
			})
		}
	}

	// 4. sing-box / warp
	if warp.Enabled || len(inbounds) == 0 {
		runtimes = append(runtimes, ManagedRuntime{
			Name:             "sing-box",
			ActionName:       "sing-box",
			Unit:             renderer.UnitWarp,
			PromotedSubpath:  generatedconfig.WarpConfigSubpath,
			PromotedVerb:     "reload",
			ManualRestart:    true,
			HealthCheckAfter: true,
		})
	}

	// 5. mieru (aggregated)
	if hasMieru {
		runtimes = append(runtimes, ManagedRuntime{
			Name:             "mieru",
			ActionName:       "mieru",
			Protocol:         "mieru",
			Unit:             renderer.UnitMieru,
			PromotedSubpath:  generatedconfig.MieruConfigSubpath,
			PromotedVerb:     "restart",
			ManualRestart:    true,
			HealthCheckAfter: true,
		})
	}

	sort.SliceStable(runtimes, func(i, j int) bool {
		// keep veil first
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
