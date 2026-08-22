package naiveproxy

import (
	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/runtimeinstall"
	"github.com/mikkelchokolate/Veil/internal/service"
)

const templateUnit = "veil-caddy.service"

// RuntimeDescriptors returns the single veil-caddy.service runtime for all
// naiveproxy inbounds. The inbound/Caddy redesign consolidates Caddy into one
// process backed by a single JSON config. When called with nil inbounds (the
// broad managed-unit catalog), it still returns the Caddy runtime so install
// and uninstall paths know about the consolidated unit.
func (p Plugin) RuntimeDescriptors(enabledInbounds []model.Inbound) []service.ManagedRuntime {
	if len(enabledInbounds) == 0 {
		// In the broad fallback catalog the consolidated Caddy runtime is not
		// tied to a specific protocol; it exists so service actions and orphan
		// cleanup can reference the canonical unit.
		return []service.ManagedRuntime{{
			Name:             "veil-caddy.service",
			ActionName:       "caddy",
			Transport:        "tcp",
			Unit:             "veil-caddy.service",
			TemplateUnit:     templateUnit,
			PromotedSubpath:  generatedconfig.CaddyJSONConfigSubpath,
			PromotedVerb:     "reload",
			ManualRestart:    true,
			HealthCheckAfter: true,
		}}
	}
	for _, inb := range enabledInbounds {
		if inb.Protocol == "naiveproxy" {
			return []service.ManagedRuntime{{
				Name:             "veil-caddy.service",
				ActionName:       "caddy",
				Protocol:         p.Protocol(),
				Transport:        "tcp",
				Unit:             "veil-caddy.service",
				TemplateUnit:     templateUnit,
				PromotedSubpath:  generatedconfig.CaddyJSONConfigSubpath,
				PromotedVerb:     "reload",
				ManualRestart:    true,
				HealthCheckAfter: true,
			}}
		}
	}
	return nil
}

// RuntimeInstall returns the Caddy-with-naive runtime descriptor.
func (Plugin) RuntimeInstall(string) runtimeinstall.Runtime {
	return runtimeinstall.Runtime{
		Name:           "naiveproxy",
		Binary:         "caddy",
		Method:         runtimeinstall.MethodCaddyNaive,
		Version:        "v2.11.4",
		Integrity:      "reproducible-go-build",
		VersionArgs:    []string{"version"},
		VersionCommand: "caddy version",
		VersionPattern: `(?i)2\.11\.4`,
		Description:    "caddy is built from source with the naive forwardproxy fork",
	}
}
