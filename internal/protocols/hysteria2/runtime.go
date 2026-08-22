package hysteria2

import (
	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/runtimeinstall"
	"github.com/mikkelchokolate/Veil/internal/service"
)

const templateUnit = "veil-hysteria2@.service"

// RuntimeDescriptors returns per-inbound Hysteria2 units.
func (p Plugin) RuntimeDescriptors(enabledInbounds []model.Inbound) []service.ManagedRuntime {
	var runtimes []service.ManagedRuntime
	count := 0
	for _, inbound := range enabledInbounds {
		if inbound.Protocol != p.Protocol() {
			continue
		}
		count++
		runtimes = append(runtimes, service.ManagedRuntime{
			Name:             "hysteria2-" + inbound.Name,
			ActionName:       "hysteria2-" + inbound.Name,
			Protocol:         p.Protocol(),
			Transport:        "udp",
			Unit:             "veil-hysteria2@" + inbound.Name + ".service",
			TemplateUnit:     templateUnit,
			PromotedSubpath:  "hysteria2/" + inbound.Name + ".yaml",
			PromotedVerb:     "restart",
			ManualRestart:    true,
			HealthCheckAfter: true,
		})
	}
	if count == 0 {
		runtimes = append(runtimes, service.ManagedRuntime{
			Name:             "hysteria2",
			ActionName:       "hysteria2",
			Protocol:         p.Protocol(),
			Transport:        "udp",
			Unit:             templateUnit,
			TemplateUnit:     templateUnit,
			PromotedSubpath:  generatedconfig.Hysteria2ConfigSubpath,
			PromotedVerb:     "restart",
			ManualRestart:    true,
			HealthCheckAfter: true,
		})
	}
	return runtimes
}

// RuntimeInstall returns the Hysteria2 runtime descriptor.
func (Plugin) RuntimeInstall(arch string) runtimeinstall.Runtime {
	return runtimeinstall.Runtime{
		Name:           "hysteria2",
		Binary:         "hysteria",
		Method:         runtimeinstall.MethodRawBinary,
		Repo:           "apernet/hysteria",
		Version:        "app/v2.12.1",
		Integrity:      "upstream-checksum",
		VersionArgs:    []string{"version"},
		VersionCommand: "hysteria version",
		VersionPattern: `(?i)2\.12\.1`,
		Description:    "hysteria is downloaded from its upstream GitHub release",
		AssetMatch: func(name string) bool {
			return name == "hysteria-linux-"+arch
		},
		ChecksumMatch: func(name string) bool {
			return name == "hashes.txt"
		},
	}
}
