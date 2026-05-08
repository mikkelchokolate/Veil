package api

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestGeneratedConfigSetAggregatesMieruInboundsIntoOneServerConfig(t *testing.T) {
	applyRoot := t.TempDir()
	configs, err := BuildGeneratedConfigSet(GeneratedConfigInput{
		ApplyRoot: applyRoot,
		Settings:  Settings{},
		Inbounds: []Inbound{
			{Name: "mieru-tcp", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Password: "tcp-pass"},
			{Name: "mieru-udp", Protocol: "mieru", Transport: "udp", Port: 443, Enabled: true, Profiles: []ClientProfile{{Name: "alice", Password: "alice-pass", Enabled: true}}},
		},
	})
	if err != nil {
		t.Fatalf("BuildGeneratedConfigSet: %v", err)
	}
	body, ok := configs[filepath.Join(applyRoot, "generated", "mieru", "server_config.json")]
	if !ok {
		t.Fatalf("missing Mieru config: %+v", configs)
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
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("invalid Mieru JSON: %v\n%s", err, body)
	}
	if len(decoded.PortBindings) != 2 || decoded.PortBindings[0].Protocol != "TCP" || decoded.PortBindings[1].Protocol != "UDP" || decoded.PortBindings[0].Port != 443 || decoded.PortBindings[1].Port != 443 {
		t.Fatalf("port bindings = %+v", decoded.PortBindings)
	}
	if len(decoded.Users) != 2 || decoded.Users[0].Name != "mieru-tcp" || decoded.Users[0].Password != "tcp-pass" || decoded.Users[1].Name != "alice" || decoded.Users[1].Password != "alice-pass" {
		t.Fatalf("users = %+v", decoded.Users)
	}
}
