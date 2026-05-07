package api

import "testing"

func TestStatusResponseBuilderBuildsVersionModeAndServices(t *testing.T) {
	response := NewStatusResponseBuilder(ServerInfo{Version: "1.2.3", Mode: "server"}, func() []ServiceStatus {
		return []ServiceStatus{{Name: "veil", Managed: true}}
	}).Build()
	if response.SchemaVersion != "v1" || response.Name != "Veil" || response.Version != "1.2.3" || response.Mode != "server" {
		t.Fatalf("response = %+v", response)
	}
	if len(response.Services) != 1 || response.Services[0].Name != "veil" {
		t.Fatalf("services = %+v", response.Services)
	}
}
