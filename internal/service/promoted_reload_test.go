package service

import (
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestPromotedServiceReloaderStopsOnFirstFailure(t *testing.T) {
	catalog := NewManagedRuntimeCatalog([]ManagedRuntime{
		{Name: "mieru", Unit: "veil-mieru.service", PromotedSubpath: "mieru/server_config.json", PromotedVerb: "restart"},
		{Name: "sing-box", Unit: "veil-warp.service", PromotedSubpath: "sing-box/warp.json", PromotedVerb: "reload"},
	})
	calls := 0
	reloader := NewPromotedServiceReloader(filepath.FromSlash("/etc/veil"), catalog, func(command []string) model.ServiceActionResult {
		calls++
		return model.ServiceActionResult{Name: command[2], Command: command, Success: false, Error: "boom"}
	})
	results := reloader.Reload([]string{
		filepath.FromSlash("/etc/veil/live/mieru/server_config.json"),
		filepath.FromSlash("/etc/veil/live/sing-box/warp.json"),
	})
	if calls != 1 || len(results) != 1 || results[0].Name != "veil-mieru.service" {
		t.Fatalf("calls=%d results=%+v", calls, results)
	}
}
