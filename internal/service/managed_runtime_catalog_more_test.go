package service

import (
	"path/filepath"
	"testing"
)

func TestManagedRuntimeCatalogCopyAndRuntimes(t *testing.T) {
	input := []ManagedRuntime{{Name: "a", Unit: "a.service"}}
	catalog := NewManagedRuntimeCatalog(input)
	input[0].Name = "mutated"
	runtimes := catalog.Runtimes()
	if runtimes[0].Name != "a" {
		t.Fatal("NewManagedRuntimeCatalog did not copy input slice")
	}
	runtimes[0].Name = "mutated-runtime"
	if catalog.Runtimes()[0].Name != "a" {
		t.Fatal("Runtimes did not return a copy")
	}
}

func TestApplyActionSkipsEmptyPromotedVerb(t *testing.T) {
	catalog := NewManagedRuntimeCatalog([]ManagedRuntime{
		{Name: "a", Protocol: "a", ActionName: "a", Unit: "a.service", PromotedVerb: ""},
	})
	if _, ok := catalog.ApplyAction("a"); ok {
		t.Fatal("expected ApplyAction to skip runtimes with empty PromotedVerb")
	}
}

func TestApplyActionMatchesByProtocolNameAndActionName(t *testing.T) {
	catalog := NewManagedRuntimeCatalog([]ManagedRuntime{
		{Name: "naive", Protocol: "naiveproxy", ActionName: "naive", Unit: "veil-naive.service", PromotedVerb: "reload"},
	})
	tests := []struct {
		key  string
		want string
	}{
		{"naiveproxy", "reload veil-naive.service"},
		{"naive", "reload veil-naive.service"},
		{"naive", "reload veil-naive.service"},
	}
	for _, tt := range tests {
		if action, ok := catalog.ApplyAction(tt.key); !ok || action != tt.want {
			t.Fatalf("ApplyAction(%q) = %q %v, want %q true", tt.key, action, ok, tt.want)
		}
	}
}

func TestAllowsActionName(t *testing.T) {
	catalog := NewManagedRuntimeCatalog([]ManagedRuntime{
		{Name: "mieru", ActionName: "mieru", Unit: "veil-mieru.service", ManualRestart: true},
	})
	if !catalog.AllowsActionName("mieru") {
		t.Fatal("expected AllowsActionName to return true")
	}
	if catalog.AllowsActionName("caddy") {
		t.Fatal("expected AllowsActionName to return false for unknown")
	}
	if catalog.AllowsActionName("mieru-no-restart") {
		t.Fatal("expected AllowsActionName to return false when ManualRestart is false")
	}
}

func TestServiceActionCommandNotFound(t *testing.T) {
	catalog := NewManagedRuntimeCatalog([]ManagedRuntime{
		{Name: "mieru", ActionName: "mieru", Unit: "veil-mieru.service"},
	})
	if _, ok := catalog.ServiceActionCommand("mieru", "restart"); ok {
		t.Fatal("expected ServiceActionCommand to return false when ManualRestart is false")
	}
	if _, ok := catalog.ServiceActionCommand("caddy", "restart"); ok {
		t.Fatal("expected ServiceActionCommand to return false for unknown action")
	}
}

func TestAllowsPromotedActionStandardVerbs(t *testing.T) {
	catalog := NewManagedRuntimeCatalog([]ManagedRuntime{})
	tests := []struct {
		command []string
		want    bool
	}{
		{[]string{"systemctl", "start", "veil-caddy@x.service"}, true},
		{[]string{"systemctl", "stop", "veil-hysteria2@y.service"}, true},
		{[]string{"systemctl", "enable", "veil-olcrtc@z.service"}, true},
		{[]string{"systemctl", "disable", "veil-caddy@x.service"}, true},
		{[]string{"systemctl", "restart", "veil-caddy@x.service"}, false},
		{[]string{"systemctl", "start", "veil-unknown@x.service"}, false},
		{[]string{"systemctl", "start", "veil-caddy@x"}, false},
		{[]string{"systemctl", "kill", "veil-caddy@x.service"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.command[1]+"_"+tt.command[2], func(t *testing.T) {
			if got := catalog.AllowsPromotedAction(tt.command); got != tt.want {
				t.Fatalf("AllowsPromotedAction(%v) = %v, want %v", tt.command, got, tt.want)
			}
		})
	}
}

func TestMatchUnitTemplateSuffix(t *testing.T) {
	catalog := NewManagedRuntimeCatalog([]ManagedRuntime{
		{Unit: "veil-caddy@.service", HealthCheckAfter: true},
	})
	if !catalog.AllowsHealthUnit("veil-caddy@x.service") {
		t.Fatal("expected template unit to match instantiated unit")
	}
	if catalog.AllowsHealthUnit("veil-caddy@x.servic") {
		t.Fatal("expected suffix mismatch to be rejected")
	}
	if catalog.AllowsHealthUnit("veil-hysteria2@x.service") {
		t.Fatal("expected prefix mismatch to be rejected")
	}
}

func TestContainsPathFalse(t *testing.T) {
	catalog := NewManagedRuntimeCatalog([]ManagedRuntime{
		{Name: "mieru", Unit: "veil-mieru.service", PromotedSubpath: "mieru/config.json", PromotedVerb: "restart"},
	})
	commands := catalog.PromotedCommands(filepath.FromSlash("/etc/veil"), []string{
		filepath.FromSlash("/etc/veil/live/sing-box/config.json"),
	})
	if len(commands) != 0 {
		t.Fatalf("expected no promoted commands, got %+v", commands)
	}
}
