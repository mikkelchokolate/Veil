package api

import "testing"

func TestManagedServiceStatusCatalogIncludesMieru(t *testing.T) {
	services := NewManagedServiceStatusCatalog(func(unit string) ServiceRuntimeStatus { return ServiceRuntimeStatus{Unit: unit} }).List()
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
	if !(ServiceCommandPolicy{}).AllowsHealth("veil-mieru.service") {
		t.Fatal("veil-mieru.service should be allowed")
	}
}
