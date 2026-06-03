package api

import (
	"sort"

	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/protocols"
	"github.com/mikkelchokolate/Veil/internal/renderer"
	"github.com/mikkelchokolate/Veil/internal/service"
)

type ManagedRuntime = service.ManagedRuntime
type ManagedRuntimeCatalog = service.ManagedRuntimeCatalog

func NewManagedRuntimeCatalog() ManagedRuntimeCatalog {
	runtimes := []ManagedRuntime{{Name: "veil", ActionName: "veil", Unit: renderer.UnitVeil, ManualRestart: true}}
	ordered := []struct {
		Order   int
		Runtime ManagedRuntime
	}{}
	for _, capability := range protocols.NewCapabilityCatalog().All() {
		if capability.RuntimeUnit == "" {
			continue
		}
		ordered = append(ordered, struct {
			Order   int
			Runtime ManagedRuntime
		}{
			Order: capability.RuntimeOrder,
			Runtime: ManagedRuntime{
				Name:             capability.RuntimeName,
				ActionName:       capability.RuntimeActionName,
				Protocol:         capability.Protocol,
				Transport:        capability.RuntimeTransport,
				Unit:             capability.RuntimeUnit,
				PromotedSubpath:  capability.GeneratedConfig.Subpath,
				PromotedVerb:     capability.PromotedVerb,
				ManualRestart:    true,
				HealthCheckAfter: true,
			},
		})
	}
	ordered = append(ordered, struct {
		Order   int
		Runtime ManagedRuntime
	}{Order: 30, Runtime: ManagedRuntime{Name: "sing-box", ActionName: "sing-box", Unit: renderer.UnitWarp, PromotedSubpath: generatedconfig.WarpConfigSubpath, PromotedVerb: "reload", ManualRestart: true, HealthCheckAfter: true}})

	hasOlcrtc := false
	for _, item := range ordered {
		if item.Runtime.Name == "olcrtc" {
			hasOlcrtc = true
			break
		}
	}
	if !hasOlcrtc {
		ordered = append(ordered, struct {
			Order   int
			Runtime ManagedRuntime
		}{
			Order: 30,
			Runtime: ManagedRuntime{
				Name:             "olcrtc",
				ActionName:       "olcrtc",
				Protocol:         "olcrtc",
				Transport:        "udp",
				Unit:             "veil-olcrtc.service",
				PromotedSubpath:  "olcrtc/server.yaml",
				PromotedVerb:     "restart",
				ManualRestart:    true,
				HealthCheckAfter: true,
			},
		})
	}

	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Order < ordered[j].Order })
	for _, item := range ordered {
		runtimes = append(runtimes, item.Runtime)
	}
	return service.NewManagedRuntimeCatalog(runtimes)
}
