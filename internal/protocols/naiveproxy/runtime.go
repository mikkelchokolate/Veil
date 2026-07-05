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
// process backed by a single JSON config.
func (p Plugin) RuntimeDescriptors(enabledInbounds []model.Inbound) []service.ManagedRuntime {
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
		Name:   "naiveproxy",
		Binary: "caddy",
		Method: runtimeinstall.MethodCaddyNaive,
	}
}
