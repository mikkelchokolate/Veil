package api

import "testing"

func TestManagedServiceStatusCatalogBuildsKnownManagedServices(t *testing.T) {
	catalog := NewManagedServiceStatusCatalog(func(unit string) ServiceRuntimeStatus {
		return ServiceRuntimeStatus{Unit: unit, LoadState: "loaded", ActiveState: "active", SubState: "running"}
	})
	services := catalog.List()
	if len(services) != 5 {
		t.Fatalf("services = %+v", services)
	}
	want := []struct{ name, unit string }{{"veil", "veil.service"}, {"naive", "veil-naive.service"}, {"hysteria2", "veil-hysteria2.service"}, {"sing-box", "veil-warp.service"}, {"mieru", "veil-mieru.service"}}
	for i, expected := range want {
		if services[i].Name != expected.name || services[i].Unit != expected.unit || !services[i].Managed || services[i].ActiveState != "active" {
			t.Fatalf("service[%d] = %+v", i, services[i])
		}
	}
}
