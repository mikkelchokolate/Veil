package api

import "testing"

func TestRouterCompositionBuildsHandlerAndReloader(t *testing.T) {
	handler, reloader := NewRouterComposition(ServerInfo{Version: "test", Mode: "dev", WebBasePath: "/secret/"}).Build()
	if handler == nil {
		t.Fatalf("handler is nil")
	}
	if reloader == nil {
		t.Fatalf("reloader is nil")
	}
}
