package generatedconfig

import (
	"encoding/json"
	"testing"
)

func TestGeneratedMieruConfigRendererAggregatesBindingsIntoOneServerConfig(t *testing.T) {
	applyRoot := t.TempDir()
	artifact, ok, err := NewGeneratedMieruConfigRenderer(Settings{}, NewPaths(applyRoot)).Render([]Inbound{
		{Name: "mieru-tcp", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Password: "tcp-pass"},
		{Name: "mieru-udp", Protocol: "mieru", Transport: "udp", Port: 443, Enabled: true, Profiles: []ClientProfile{{Name: "alice", Password: "alice-pass", Enabled: true}}},
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !ok || artifact.Path != NewPaths(applyRoot).Mieru() {
		t.Fatalf("artifact = %+v ok=%v", artifact, ok)
	}
	var decoded struct {
		PortBindings []struct {
			Port     int    `json:"port"`
			Protocol string `json:"protocol"`
		} `json:"portBindings"`
		Users []struct {
			Name     string `json:"name"`
			Password string `json:"password"`
		} `json:"users"`
	}
	if err := json.Unmarshal([]byte(artifact.Body), &decoded); err != nil {
		t.Fatalf("invalid Mieru JSON: %v\n%s", err, artifact.Body)
	}
	if len(decoded.PortBindings) != 2 || decoded.PortBindings[0].Protocol != "TCP" || decoded.PortBindings[1].Protocol != "UDP" {
		t.Fatalf("port bindings = %+v", decoded.PortBindings)
	}
	if len(decoded.Users) != 2 || decoded.Users[0].Name != "mieru-tcp" || decoded.Users[1].Name != "alice" {
		t.Fatalf("users = %+v", decoded.Users)
	}
}
