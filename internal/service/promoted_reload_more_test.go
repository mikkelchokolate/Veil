package service

import (
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestPromotedServiceReloaderFillsMissingResultFields(t *testing.T) {
	catalog := NewManagedRuntimeCatalog([]ManagedRuntime{
		{Name: "mieru", Unit: "veil-mieru.service", PromotedSubpath: "mieru/config.json", PromotedVerb: "restart"},
	})
	reloader := NewPromotedServiceReloader(filepath.FromSlash("/etc/veil"), catalog, func(command []string) model.ServiceActionResult {
		return model.ServiceActionResult{Success: true}
	})
	results := reloader.Reload([]string{filepath.FromSlash("/etc/veil/live/mieru/config.json")})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %+v", results)
	}
	if results[0].Name != "veil-mieru.service" {
		t.Fatalf("Name = %q, want veil-mieru.service", results[0].Name)
	}
	wantCommand := []string{"systemctl", "restart", "veil-mieru.service"}
	if !equalStrings(results[0].Command, wantCommand) {
		t.Fatalf("Command = %v, want %v", results[0].Command, wantCommand)
	}
}

func TestPromotedServiceReloaderRunsAllOnSuccess(t *testing.T) {
	catalog := NewManagedRuntimeCatalog([]ManagedRuntime{
		{Name: "mieru", Unit: "veil-mieru.service", PromotedSubpath: "mieru/config.json", PromotedVerb: "restart"},
		{Name: "sing-box", Unit: "veil-warp.service", PromotedSubpath: "sing-box/warp.json", PromotedVerb: "reload"},
	})
	callCount := 0
	reloader := NewPromotedServiceReloader(filepath.FromSlash("/etc/veil"), catalog, func(command []string) model.ServiceActionResult {
		callCount++
		return model.ServiceActionResult{Name: command[2], Command: command, Success: true}
	})
	results := reloader.Reload([]string{
		filepath.FromSlash("/etc/veil/live/mieru/config.json"),
		filepath.FromSlash("/etc/veil/live/sing-box/warp.json"),
	})
	if callCount != 2 || len(results) != 2 {
		t.Fatalf("callCount=%d results=%+v", callCount, results)
	}
}

func TestPromotedServiceActionCatalogNoMatches(t *testing.T) {
	catalog := NewManagedRuntimeCatalog([]ManagedRuntime{
		{Name: "mieru", Unit: "veil-mieru.service", PromotedSubpath: "mieru/config.json", PromotedVerb: "restart"},
	})
	cat := NewPromotedServiceActionCatalog(filepath.FromSlash("/etc/veil"), catalog)
	commands := cat.Commands([]string{filepath.FromSlash("/etc/veil/live/sing-box/warp.json")})
	if len(commands) != 0 {
		t.Fatalf("expected no commands, got %+v", commands)
	}
}
