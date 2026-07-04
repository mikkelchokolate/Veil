package renderer

import (
	"encoding/json"
	"testing"
)

func TestRenderMieruClientMultiplePortBindings(t *testing.T) {
	body, err := RenderMieruClient(MieruClientConfig{
		ProfileName:   "mieru/bob",
		DomainName:    "vpn.example.com",
		PortBindings:  []MieruPortBinding{{Port: 443, Protocol: "tcp"}, {Port: 443, Protocol: "udp"}},
		User:          MieruUser{Name: "bob", Password: "bob-pass"},
		Socks5Port:    1080,
		HTTPProxyPort: 8080,
		RPCPort:       9000,
	})
	if err != nil {
		t.Fatalf("RenderMieruClient: %v", err)
	}
	var decoded struct {
		ActiveProfile string `json:"activeProfile"`
		Profiles      []struct {
			ProfileName string `json:"profileName"`
			Servers     []struct {
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
	if decoded.ActiveProfile != "mieru/bob" {
		t.Fatalf("activeProfile = %q", decoded.ActiveProfile)
	}
	bindings := decoded.Profiles[0].Servers[0].PortBindings
	if len(bindings) != 2 || bindings[0].Protocol != "TCP" || bindings[1].Protocol != "UDP" {
		t.Fatalf("bindings = %+v", bindings)
	}
	if decoded.Socks5Port != 1080 || decoded.HTTPProxyPort != 8080 || decoded.RPCPort != 9000 {
		t.Fatalf("ports = %d/%d/%d", decoded.Socks5Port, decoded.HTTPProxyPort, decoded.RPCPort)
	}
}
