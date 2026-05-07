package renderer

import (
	"encoding/json"
	"testing"
)

func TestRenderMieruClientConfigBuildsClientJSON(t *testing.T) {
	body, err := RenderMieruClient(MieruClientConfig{
		ProfileName:  "mieru/alice",
		DomainName:   "vpn.example.com",
		PortBindings: []MieruPortBinding{{Port: 443, Protocol: "tcp"}},
		User:         MieruUser{Name: "alice", Password: "alice-pass"},
	})
	if err != nil {
		t.Fatalf("RenderMieruClient: %v", err)
	}
	var decoded struct {
		ProfileName string `json:"profileName"`
		User        struct {
			Name     string `json:"name"`
			Password string `json:"password"`
		} `json:"user"`
		Servers []struct {
			DomainName   string `json:"domainName"`
			PortBindings []struct {
				Port     int    `json:"port"`
				Protocol string `json:"protocol"`
			} `json:"portBindings"`
		} `json:"servers"`
	}
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, body)
	}
	if decoded.ProfileName != "mieru/alice" || decoded.User.Name != "alice" || decoded.User.Password != "alice-pass" || decoded.Servers[0].DomainName != "vpn.example.com" || decoded.Servers[0].PortBindings[0].Protocol != "TCP" {
		t.Fatalf("decoded = %+v", decoded)
	}
}
