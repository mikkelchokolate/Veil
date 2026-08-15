package olcrtc

import (
	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
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
			PromotedSubpath:  generatedconfig.OlcrtcConfigSubpath,
			PromotedVerb:     "restart",
			ManualRestart:    true,
			HealthCheckAfter: true,
		})
	}
	return runtimes
}

// RuntimeInstall returns the olcRTC runtime descriptor. Keep this source pin in
// lockstep with the wire format consumed by current olcRTC clients: the current
// OLC2 record layer has no compatibility fallback to the legacy pre-OLC2 wire.
func (Plugin) RuntimeInstall(string) runtimeinstall.Runtime {
	const sourceCommit = "48cae636f88e16863c99d4147bbc327a856cdf00"
	return runtimeinstall.Runtime{
		Name:           "olcrtc",
		Binary:         "olcrtc",
		Method:         runtimeinstall.MethodGoInstall,
		SourcePackage:  "github.com/openlibrecommunity/olcrtc/cmd/olcrtc@" + sourceCommit,
		Version:        sourceCommit,
		SourceCommit:   sourceCommit,
		Integrity:      "go-module-sum",
		VersionArgs:    []string{"__go_buildinfo__"},
		VersionCommand: "go version -m olcrtc",
		VersionPattern: `48cae636f88e`,
		Description:    "olcrtc is built from source with \"go install\"",
	}
}
