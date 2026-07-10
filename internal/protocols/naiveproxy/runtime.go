package naiveproxy

import (
	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/runtimeinstall"
	"github.com/mikkelchokolate/Veil/internal/service"
)

const templateUnit = "veil-caddy@.service"

// RuntimeDescriptors returns per-inbound caddy units for naiveproxy.
func (p Plugin) RuntimeDescriptors(enabledInbounds []model.Inbound) []service.ManagedRuntime {
	var runtimes []service.ManagedRuntime
	count := 0
	for _, inbound := range enabledInbounds {
		if inbound.Protocol != p.Protocol() {
			continue
		}
		count++
		runtimes = append(runtimes, service.ManagedRuntime{
			Name:             "caddy-" + inbound.Name,
			ActionName:       "caddy-" + inbound.Name,
			Protocol:         p.Protocol(),
			Transport:        "tcp",
			Unit:             "veil-caddy@" + inbound.Name + ".service",
			TemplateUnit:     templateUnit,
			PromotedSubpath:  "caddy/" + inbound.Name + ".Caddyfile",
			PromotedVerb:     "reload",
			ManualRestart:    true,
			HealthCheckAfter: true,
		})
	}
	if count == 0 {
		runtimes = append(runtimes, service.ManagedRuntime{
			Name:             "caddy-panel",
			ActionName:       "caddy-panel",
			Protocol:         p.Protocol(),
			Transport:        "tcp",
			Unit:             "veil-caddy@panel.service",
			TemplateUnit:     templateUnit,
			PromotedSubpath:  generatedconfig.CaddyfileSubpath,
			PromotedVerb:     "reload",
			ManualRestart:    true,
			HealthCheckAfter: true,
		})
	}
	return runtimes
}

// RuntimeInstall returns the Caddy-with-naive runtime descriptor.
func (Plugin) RuntimeInstall(string) runtimeinstall.Runtime {
	return runtimeinstall.Runtime{
		Name:        "naiveproxy",
		Binary:      "caddy",
		Method:      runtimeinstall.MethodCaddyNaive,
		Description: "caddy is built from source with the naive forwardproxy fork",
	}
}
