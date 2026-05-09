package service

import "testing"

func TestManagedRuntimeCatalogBuildsPromotedCommandsAndPolicies(t *testing.T) {
	catalog := NewManagedRuntimeCatalog([]ManagedRuntime{
		{Name: "veil", ActionName: "veil", Unit: "veil.service", ManualRestart: true},
		{Name: "mieru", ActionName: "mieru", Protocol: "mieru", Unit: "veil-mieru.service", PromotedSubpath: "mieru/server_config.json", PromotedVerb: "restart", ManualRestart: true, HealthCheckAfter: true},
	})
	commands := catalog.PromotedCommands("/etc/veil", []string{"/etc/veil/live/mieru/server_config.json"})
	if len(commands) != 1 || commands[0][0] != "systemctl" || commands[0][1] != "restart" || commands[0][2] != "veil-mieru.service" {
		t.Fatalf("commands = %+v", commands)
	}
	if action, ok := catalog.ApplyAction("mieru"); !ok || action != "restart veil-mieru.service" {
		t.Fatalf("ApplyAction = %q %v", action, ok)
	}
	if !catalog.AllowsPromotedAction([]string{"systemctl", "restart", "veil-mieru.service"}) || !catalog.AllowsHealthUnit("veil-mieru.service") {
		t.Fatalf("catalog did not allow promoted action/health")
	}
}
