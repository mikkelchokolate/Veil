package olcrtc

import (
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/runtimeinstall"
	"github.com/mikkelchokolate/Veil/internal/service"
)

const templateUnit = "veil-olcrtc@.service"

// RuntimeDescriptors returns per-inbound olcRTC units.
func (p Plugin) RuntimeDescriptors(enabledInbounds []model.Inbound) []service.ManagedRuntime {
	var runtimes []service.ManagedRuntime
	count := 0
	for _, inbound := range enabledInbounds {
		if inbound.Protocol != p.Protocol() {
			continue
		}
		count++
		runtimes = append(runtimes, service.ManagedRuntime{
			Name:             "olcrtc-" + inbound.Name,
			ActionName:       "olcrtc-" + inbound.Name,
			Protocol:         p.Protocol(),
			Transport:        "udp",
			Unit:             "veil-olcrtc@" + inbound.Name + ".service",
			TemplateUnit:     templateUnit,
			PromotedSubpath:  "olcrtc/" + inbound.Name + ".yaml",
			PromotedVerb:     "restart",
			ManualRestart:    true,
			HealthCheckAfter: true,
		})
	}
	if count == 0 {
		runtimes = append(runtimes, service.ManagedRuntime{
			Name:             "olcrtc",
			ActionName:       "olcrtc",
			Protocol:         p.Protocol(),
			Transport:        "udp",
			Unit:             templateUnit,
			TemplateUnit:     templateUnit,
			PromotedSubpath:  "olcrtc/server.yaml",
			PromotedVerb:     "restart",
			ManualRestart:    true,
			HealthCheckAfter: true,
		})
	}
	return runtimes
}

// RuntimeInstall returns the olcRTC runtime descriptor.
func (Plugin) RuntimeInstall(string) runtimeinstall.Runtime {
	return runtimeinstall.Runtime{
		Name:          "olcrtc",
		Binary:        "olcrtc",
		Method:        runtimeinstall.MethodGoInstall,
		SourcePackage: "github.com/openlibrecommunity/olcrtc/cmd/olcrtc@latest",
	}
}
