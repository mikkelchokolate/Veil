package service

import (
	"path/filepath"
	"reflect"
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
	var got [][]string
	reloader := NewPromotedServiceReloader(filepath.FromSlash("/etc/veil"), catalog, func(command []string) model.ServiceActionResult {
		got = append(got, append([]string(nil), command...))
		return model.ServiceActionResult{Name: command[2], Command: command, Success: true}
	})
	results := reloader.Reload([]string{
		filepath.FromSlash("/etc/veil/live/mieru/config.json"),
		filepath.FromSlash("/etc/veil/live/sing-box/warp.json"),
	})
	wantCommands := [][]string{
		{"systemctl", "restart", "veil-mieru.service"},
		{"systemctl", "reload", "veil-warp.service"},
	}
	if !reflect.DeepEqual(got, wantCommands) {
		t.Fatalf("commands = %v, want %v", got, wantCommands)
	}
	wantNames := []string{"veil-mieru.service", "veil-warp.service"}
	if len(results) != len(wantNames) {
		t.Fatalf("results = %+v", results)
	}
	for i, want := range wantNames {
		if results[i].Name != want {
			t.Fatalf("results[%d].Name = %q, want %q", i, results[i].Name, want)
		}
		if !reflect.DeepEqual(results[i].Command, wantCommands[i]) {
			t.Fatalf("results[%d].Command = %v, want %v", i, results[i].Command, wantCommands[i])
		}
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
