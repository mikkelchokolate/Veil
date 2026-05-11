package protocols

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/veil-panel/veil/internal/generatedconfig"
	"github.com/veil-panel/veil/internal/model"
)

func TestCapabilityCatalogCoversMieruEndToEnd(t *testing.T) {
	capability, ok := NewCapabilityCatalog().ForProtocol("mieru")
	if !ok {
		t.Fatal("missing Mieru protocol capability")
	}
	if capability.GeneratedConfig.PlanPath() != "/etc/veil/generated/mieru/server_config.json" || capability.ApplyAction != "restart veil-mieru.service" || capability.RuntimeUnit != "veil-mieru.service" || capability.PromotedVerb != "restart" || capability.RenderGeneratedConfig == nil {
		t.Fatalf("Mieru generated/apply/runtime capability = %+v", capability)
	}
	if len(capability.Transports) != 2 || capability.Transports[0] != "tcp" || capability.Transports[1] != "udp" {
		t.Fatalf("Mieru transports = %+v", capability.Transports)
	}
	validation, ok := capability.GeneratedConfig.ValidationSpec("/etc/veil/generated/mieru/server_config.json")
	if !ok {
		t.Fatal("missing Mieru validation spec")
	}
	cmd := validation.Command
	if len(cmd) != 4 || cmd[0] != "mieru" || cmd[1] != "check" || cmd[2] != "-c" {
		t.Fatalf("Mieru validation command = %+v", cmd)
	}
}

func TestGeneratedConfigRegistryOwnsCardinalityAndMieruRendering(t *testing.T) {
	registry := NewGeneratedConfigRegistry()
	err := registry.Validate(model.Settings{}, []model.Inbound{
		{Name: "naive-a", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true},
		{Name: "naive-b", Protocol: "naiveproxy", Transport: "tcp", Port: 8443, Enabled: true},
	})
	if err == nil || !strings.Contains(err.Error(), "multiple enabled naiveproxy inbounds") {
		t.Fatalf("expected single-config protocol cardinality error, got %v", err)
	}
	artifact, ok, err := registry.RenderInbound(model.Settings{}, generatedconfig.NewPaths(t.TempDir()), model.Inbound{Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: 9443, Enabled: true, Password: "secret"})
	if err != nil {
		t.Fatalf("RenderInbound: %v", err)
	}
	if !ok || !strings.Contains(artifact.Body, `"protocol": "TCP"`) || !strings.Contains(artifact.Body, `"password": "secret"`) {
		t.Fatalf("single inbound artifact = %+v ok=%v", artifact, ok)
	}
	root := t.TempDir()
	configs, err := registry.Render(generatedconfig.ConfigInput{
		ApplyRoot: root,
		Settings:  model.Settings{},
		Inbounds:  []model.Inbound{{Name: "mieru", Protocol: "mieru", Transport: "udp", Port: 9443, Enabled: true, Password: "secret"}},
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	body := configs[filepath.Join(root, "generated", "mieru", "server_config.json")]
	if !strings.Contains(body, `"port": 9443`) || !strings.Contains(body, `"protocol": "UDP"`) {
		t.Fatalf("bad Mieru generated config:\n%s", body)
	}
}

func TestCapabilityCatalogReturnsClonedTransports(t *testing.T) {
	all := NewCapabilityCatalog().All()
	all[0].Transports[0] = "mutated"
	capability, ok := NewCapabilityCatalog().ForProtocol("naiveproxy")
	if !ok || capability.Transports[0] != "tcp" {
		t.Fatalf("catalog leaked transport slice mutation: %+v", capability)
	}
}
