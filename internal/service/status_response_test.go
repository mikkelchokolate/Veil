package service

import "testing"

func TestStatusResponseBuilderBuildsVersionModeAndServices(t *testing.T) {
	response := NewStatusResponseBuilder(StatusInfo{Version: "1.2.3", Mode: "server"}, func() []ServiceStatus {
		return []ServiceStatus{{Name: "veil", Managed: true}}
	}).Build()
	if response.SchemaVersion != "v1" || response.Name != "Veil" || response.Version != "1.2.3" || response.Mode != "server" {
		t.Fatalf("response = %+v", response)
	}
	if len(response.Services) != 1 || response.Services[0].Name != "veil" {
		t.Fatalf("services = %+v", response.Services)
	}
}

func TestManagedServiceStatusCatalogBuildsKnownManagedServices(t *testing.T) {
	catalog := NewManagedServiceStatusCatalog(NewManagedRuntimeCatalog([]ManagedRuntime{
		{Name: "veil", Unit: "veil.service", ManualRestart: true},
		{Name: "mieru", Transport: "tcp", Unit: "veil-mieru.service", ManualRestart: true},
	}), func(unit string) RuntimeStatus {
		return RuntimeStatus{Unit: unit, LoadState: "loaded", ActiveState: "active", SubState: "running"}
	})
	services := catalog.List()
	if len(services) != 2 {
		t.Fatalf("services = %+v", services)
	}
	if services[0].Name != "veil" || services[0].Unit != "veil.service" || !services[0].Managed || services[0].ActiveState != "active" {
		t.Fatalf("services[0] = %+v", services[0])
	}
	if services[1].Name != "mieru" || services[1].Transport != "tcp" || services[1].Unit != "veil-mieru.service" {
		t.Fatalf("services[1] = %+v", services[1])
	}
}
