package panelaccess

import (
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestCaddyRouteFailsClosedForUnsafeLegacyWebBasePath(t *testing.T) {
	access := New(model.Settings{
		PanelAccess: "caddy",
		PanelListen: "127.0.0.1:2096",
		Domain:      "panel.example.com",
		Email:       "admin@example.com",
		WebBasePath: "panel\nrespond hacked",
	}, nil)
	_, ok, err := access.CaddyRoute()
	if ok {
		t.Fatal("unsafe legacy web base path unexpectedly produced a Caddy route")
	}
	if err == nil || !strings.Contains(err.Error(), "webBasePath is required") {
		t.Fatalf("expected fail-closed webBasePath error, got %v", err)
	}
}

func TestApplyIntentDoesNotScheduleUnsafePanelCaddyConfig(t *testing.T) {
	access := New(model.Settings{
		PanelAccess: "caddy",
		PanelListen: "127.0.0.1:2096",
		Domain:      "panel.example.com",
		Email:       "admin@example.com",
		WebBasePath: "panel?debug",
	}, nil)
	intent := access.ApplyIntent(nil)
	if len(intent.Configs) != 0 || len(intent.Actions) != 0 || len(intent.Runtimes) != 0 {
		t.Fatalf("unsafe web base path scheduled apply work: %+v", intent)
	}
	if len(intent.Errors) != 1 || !strings.Contains(intent.Errors[0], "webBasePath is required") {
		t.Fatalf("unexpected apply errors: %+v", intent.Errors)
	}
}
