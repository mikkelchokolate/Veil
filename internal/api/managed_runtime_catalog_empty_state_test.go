package api

import "testing"

func TestVisibleManagedRuntimeCatalogEmptyStateListsOnlyPanel(t *testing.T) {
	catalog := NewManagedRuntimeCatalogForSnapshot(Settings{}, nil, WarpConfig{})
	runtimes := catalog.Runtimes()
	if len(runtimes) != 1 {
		t.Fatalf("expected only panel runtime, got %+v", runtimes)
	}
	if runtimes[0].Name != "veil" || runtimes[0].ActionName != "veil" || runtimes[0].Unit != "veil.service" {
		t.Fatalf("runtime[0] = %+v, want veil panel runtime", runtimes[0])
	}
}

func TestVisibleManagedRuntimeCatalogDoesNotExposeUnconfiguredProtocolRuntimes(t *testing.T) {
	proto := "hysteria" + "2"
	catalog := NewManagedRuntimeCatalogForSnapshot(Settings{}, []Inbound{{Name: "vip", Protocol: proto, Enabled: true}}, WarpConfig{})
	units := map[string]bool{}
	for _, runtime := range catalog.Runtimes() {
		units[runtime.Unit] = true
	}
	for _, want := range []string{"veil.service", "veil-" + proto + "@vip.service"} {
		if !units[want] {
			t.Fatalf("expected unit %q in catalog: %+v", want, catalog.Runtimes())
		}
	}
	for _, unwanted := range []string{"veil-" + proto + "@.service", "veil-mieru.service", "veil-olcrtc@.service", "veil-warp.service"} {
		if units[unwanted] {
			t.Fatalf("unexpected unconfigured unit %q in catalog: %+v", unwanted, catalog.Runtimes())
		}
	}
}

func TestVisibleManagedRuntimeCatalogIncludesCaddyPanelOnlyForCaddyPanelAccess(t *testing.T) {
	withoutCaddy := NewManagedRuntimeCatalogForSnapshot(Settings{PanelAccess: "local"}, nil, WarpConfig{})
	if _, ok := withoutCaddy.ServiceActionCommand("caddy", "restart"); ok {
		t.Fatalf("did not expect caddy restart action without caddy panel access: %+v", withoutCaddy.Runtimes())
	}

	withCaddy := NewManagedRuntimeCatalogForSnapshot(Settings{PanelAccess: "caddy"}, nil, WarpConfig{})
	if _, ok := withCaddy.ServiceActionCommand("caddy", "restart"); !ok {
		t.Fatalf("expected caddy restart action when panel access is caddy: %+v", withCaddy.Runtimes())
	}
}
