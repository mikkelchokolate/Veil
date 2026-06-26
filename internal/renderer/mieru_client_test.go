package renderer

import (
	"encoding/json"
	"testing"
)

func TestRenderMieruClientConfigBuildsClientJSON(t *testing.T) {
	body, err := RenderMieruClient(MieruClientConfig{
		ProfileName:   "mieru/alice",
		DomainName:    "vpn.example.com",
		PortBindings:  []MieruPortBinding{{Port: 443, Protocol: "tcp"}},
		User:          MieruUser{Name: "alice", Password: "alice-pass"},
		Socks5Port:    1080,
		HTTPProxyPort: 0,
		RPCPort:       0,
	})
	if err != nil {
		t.Fatalf("RenderMieruClient: %v", err)
	}
	var decoded struct {
		ActiveProfile string `json:"activeProfile"`
		Profiles      []struct {
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
		} `json:"profiles"`
		Socks5Port    int `json:"socks5Port"`
		HTTPProxyPort int `json:"httpProxyPort"`
		RPCPort       int `json:"rpcPort"`
	}
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, body)
	}
	p := decoded.Profiles[0]
	if decoded.ActiveProfile != "mieru/alice" || decoded.Socks5Port != 1080 || p.ProfileName != "mieru/alice" || p.User.Name != "alice" || p.User.Password != "alice-pass" || p.Servers[0].DomainName != "vpn.example.com" || p.Servers[0].PortBindings[0].Protocol != "TCP" {
		t.Fatalf("decoded = %+v", decoded)
	}
}
