package api

import "github.com/veil-panel/veil/internal/service"

type ProtocolRuntimeProvisioning struct {
	inner service.ProtocolRuntimeProvisioning
}

type ProtocolRuntimeProvisioningPlan = service.ProtocolRuntimeProvisioningPlan

func NewProtocolRuntimeProvisioning() ProtocolRuntimeProvisioning {
	return ProtocolRuntimeProvisioning{inner: service.NewProtocolRuntimeProvisioning(NewManagedRuntimeCatalog())}
}

func (p ProtocolRuntimeProvisioning) Plan(inbounds []Inbound, warp WarpConfig) ProtocolRuntimeProvisioningPlan {
	return p.inner.Plan(inbounds, warp)
}
