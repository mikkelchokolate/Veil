package api

import (
	"encoding/json"
	"testing"
)

func TestBuildClientLinksIncludesMieruClientConfigForInboundPasswordFallback(t *testing.T) {
	response, err := BuildClientLinks(Settings{Domain: "vpn.example.com", Stack: "both"}, []Inbound{{Name: "mieru", Protocol: "mieru", Transport: "udp", Port: 443, Enabled: true, Password: "inbound-pass"}})
	if err != nil {
		t.Fatalf("BuildClientLinks: %v", err)
	}
	if response.Count != 1 || response.Links[0].Name != "mieru" || response.Links[0].Config == "" {
		t.Fatalf("response = %+v", response)
	}
	var config struct {
		User struct {
			Name     string `json:"name"`
			Password string `json:"password"`
		} `json:"user"`
		Servers []struct {
			PortBindings []struct {
				Protocol string `json:"protocol"`
			} `json:"portBindings"`
		} `json:"servers"`
	}
	if err := json.Unmarshal([]byte(response.Links[0].Config), &config); err != nil {
		t.Fatalf("invalid config: %v\n%s", err, response.Links[0].Config)
	}
	if config.User.Name != "mieru" || config.User.Password != "inbound-pass" || config.Servers[0].PortBindings[0].Protocol != "UDP" {
		t.Fatalf("config = %+v", config)
	}
}
