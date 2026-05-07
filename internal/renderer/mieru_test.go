package renderer

import (
	"encoding/json"
	"testing"
)

func TestRenderMieruServerConfigIncludesTCPAndUDPPortBindingsAndUsers(t *testing.T) {
	body, err := RenderMieru(MieruConfig{
		PortBindings: []MieruPortBinding{{Port: 443, Protocol: "tcp"}, {Port: 443, Protocol: "udp"}},
		Users:        []MieruUser{{Name: "alice", Password: "alice-pass"}},
	})
	if err != nil {
		t.Fatalf("RenderMieru: %v", err)
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
		t.Fatalf("invalid JSON: %v\n%s", err, body)
	}
	if len(decoded.PortBindings) != 2 || decoded.PortBindings[0].Protocol != "TCP" || decoded.PortBindings[1].Protocol != "UDP" {
		t.Fatalf("port bindings = %+v", decoded.PortBindings)
	}
	if len(decoded.Users) != 1 || decoded.Users[0].Name != "alice" || decoded.Users[0].Password != "alice-pass" {
		t.Fatalf("users = %+v", decoded.Users)
	}
}

func TestRenderMieruServerConfigRequiresPortBindingAndUsers(t *testing.T) {
	if _, err := RenderMieru(MieruConfig{}); err == nil || err.Error() != "at least one mieru port binding is required" {
		t.Fatalf("missing binding err = %v", err)
	}
	if _, err := RenderMieru(MieruConfig{PortBindings: []MieruPortBinding{{Port: 443, Protocol: "tcp"}}}); err == nil || err.Error() != "at least one mieru user is required" {
		t.Fatalf("missing user err = %v", err)
	}
}
