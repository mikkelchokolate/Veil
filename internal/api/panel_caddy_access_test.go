package api

import "testing"

func TestPanelCaddyAccessBuildsRouteFromSettings(t *testing.T) {
	route, ok, err := NewPanelCaddyAccess().Route(Settings{PanelAccess: "caddy", PanelListen: "127.0.0.1:31096", WebBasePath: "panel-secret"})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if !ok || route.Port != 31096 || route.WebBasePath != "/panel-secret/" {
		t.Fatalf("route = %+v ok=%v", route, ok)
	}
}

func TestPanelCaddyAccessSkipsNonCaddyPanelAccess(t *testing.T) {
	_, ok, err := NewPanelCaddyAccess().Route(Settings{PanelAccess: "local", PanelListen: "127.0.0.1:2096", WebBasePath: "/panel-secret/"})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if ok {
		t.Fatal("local Panel access should not produce a Caddy route")
	}
}
