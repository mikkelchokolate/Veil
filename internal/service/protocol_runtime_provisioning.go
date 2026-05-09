package service

import "github.com/veil-panel/veil/internal/model"

type ProtocolRuntimeProvisioning struct {
	catalog ManagedRuntimeCatalog
}

type ProtocolRuntimeProvisioningPlan struct {
	Runtimes []ManagedRuntime
}

func NewProtocolRuntimeProvisioning(catalog ManagedRuntimeCatalog) ProtocolRuntimeProvisioning {
	return ProtocolRuntimeProvisioning{catalog: catalog}
}

func (p ProtocolRuntimeProvisioning) Plan(inbounds []model.Inbound, warp model.WarpConfig) ProtocolRuntimeProvisioningPlan {
	selectedProtocols := map[string]bool{}
	for _, inbound := range inbounds {
		if inbound.Enabled && inbound.Protocol != "" {
			selectedProtocols[inbound.Protocol] = true
		}
	}
	runtimes := []ManagedRuntime{}
	for _, runtime := range p.catalog.Runtimes() {
		if runtime.Protocol != "" && selectedProtocols[runtime.Protocol] {
			runtimes = append(runtimes, runtime)
			continue
		}
		if warp.Enabled && runtime.Name == "sing-box" {
			runtimes = append(runtimes, runtime)
		}
	}
	return ProtocolRuntimeProvisioningPlan{Runtimes: runtimes}
}

func (p ProtocolRuntimeProvisioningPlan) SystemdUnits() []string {
	units := make([]string, 0, len(p.Runtimes))
	for _, runtime := range p.Runtimes {
		if runtime.Unit != "" {
			units = append(units, runtime.Unit)
		}
	}
	return units
}

func (p ProtocolRuntimeProvisioningPlan) RequiresRuntime(name string) bool {
	for _, runtime := range p.Runtimes {
		if runtime.Name == name || runtime.ActionName == name || runtime.Protocol == name {
			return true
		}
	}
	return false
}
