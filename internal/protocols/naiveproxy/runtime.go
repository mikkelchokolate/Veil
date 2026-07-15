package naiveproxy

import (
	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/runtimeinstall"
	"github.com/mikkelchokolate/Veil/internal/service"
)

const caddyUnit = "veil-caddy.service"

// RuntimeDescriptors returns one consolidated Caddy runtime. A nil inbound
// slice asks for the broad fallback descriptor used by policy/repair catalogs;
// a non-nil slice only contributes it when naiveproxy is enabled.
func (p Plugin) RuntimeDescriptors(enabledInbounds []model.Inbound) []service.ManagedRuntime {
	if enabledInbounds != nil {
		found := false
		for _, inbound := range enabledInbounds {
			if inbound.Enabled && inbound.Protocol == p.Protocol() {
				found = true
				break
			}
		}
		if !found {
			return nil
		}
	}
	return []service.ManagedRuntime{{
		Name:             caddyUnit,
		ActionName:       "caddy",
		Protocol:         p.Protocol(),
		Transport:        "tcp",
		Unit:             caddyUnit,
		PromotedSubpath:  generatedconfig.CaddyJSONConfigSubpath,
		PromotedVerb:     "reload",
		ManualRestart:    true,
		HealthCheckAfter: true,
	}}
}

func (Plugin) RuntimeInstall(string) runtimeinstall.Runtime {
	return runtimeinstall.Runtime{
		Name:        "naiveproxy",
		Binary:      "caddy",
		Method:      runtimeinstall.MethodCaddyNaive,
		Description: "caddy is built from source with the naive forwardproxy fork",
	}
}
