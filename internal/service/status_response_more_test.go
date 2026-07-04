package service

import (
	"reflect"
	"testing"
)

func TestNewStatusResponseBuilderDefaultsNilServices(t *testing.T) {
	builder := NewStatusResponseBuilder(StatusInfo{Version: "1.0", Mode: "server"}, nil)
	response := builder.Build()
	if response.Services != nil {
		t.Fatalf("expected nil services, got %+v", response.Services)
	}
}

func TestNewManagedServiceStatusCatalogDefaultsReader(t *testing.T) {
	catalog := NewManagedServiceStatusCatalog(NewManagedRuntimeCatalog(nil), nil)
	if catalog.read == nil {
		t.Fatal("expected default reader when nil is passed")
	}
}

func TestManagedServiceStatusCatalogListPreservesRuntimeData(t *testing.T) {
	catalog := NewManagedServiceStatusCatalog(NewManagedRuntimeCatalog([]ManagedRuntime{
		{Name: "veil", Unit: "veil.service", Transport: "tcp"},
		{Name: "empty-unit"},
	}), func(unit string) RuntimeStatus {
		return RuntimeStatus{Unit: unit, LoadState: "loaded", ActiveState: "active", SubState: "running"}
	})
	services := catalog.List()
	if len(services) != 2 {
		t.Fatalf("expected 2 services, got %+v", services)
	}
	if services[0].Name != "veil" || services[0].Managed != true || services[0].Transport != "tcp" {
		t.Fatalf("services[0] = %+v", services[0])
	}
	if !reflect.DeepEqual(services[0].LoadState, "loaded") {
		t.Fatalf("LoadState not copied: %+v", services[0])
	}
}
