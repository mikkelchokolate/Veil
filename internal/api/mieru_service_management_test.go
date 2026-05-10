package api

import (
	"testing"

	"github.com/veil-panel/veil/internal/service"
)

func TestManagedServiceStatusCatalogIncludesMieru(t *testing.T) {
	services := service.NewManagedServiceStatusCatalog(NewManagedRuntimeCatalog(), service.RuntimeStatusReader(func(unit string) ServiceRuntimeStatus { return ServiceRuntimeStatus{Unit: unit} })).List()
	found := false
	for _, svc := range services {
		if svc.Name == "mieru" && svc.Unit == "veil-mieru.service" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing Mieru managed status service: %+v", services)
	}
}

func TestServiceCommandPolicyAllowsMieruUnit(t *testing.T) {
	if !service.NewCommandPolicy(NewManagedRuntimeCatalog()).AllowsHealth("veil-mieru.service") {
		t.Fatal("veil-mieru.service should be allowed")
	}
}
