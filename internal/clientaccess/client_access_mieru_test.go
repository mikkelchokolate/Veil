package clientaccess

import (
	"encoding/json"
	"testing"
)

func TestBuildClientLinksIncludesMieruClientConfigForClientProfile(t *testing.T) {
	response, err := BuildClientLinks(Settings{Domain: "vpn.example.com"}, []Inbound{{
		Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true,
		Profiles: []ClientProfile{{Name: "alice", Password: "alice-pass", Enabled: true}},
	}})
	if err != nil {
		t.Fatalf("BuildClientLinks: %v", err)
	}
	if response.Count != 1 || response.Links[0].Protocol != "mieru" || response.Links[0].Config == "" {
		t.Fatalf("response = %+v", response)
	}
	var config struct {
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
	if err := json.Unmarshal([]byte(response.Links[0].Config), &config); err != nil {
		t.Fatalf("invalid Mieru client config: %v\n%s", err, response.Links[0].Config)
	}
	if config.ProfileName != "mieru/alice" || config.User.Name != "alice" || config.User.Password != "alice-pass" || len(config.Servers) != 1 || config.Servers[0].DomainName != "vpn.example.com" || config.Servers[0].PortBindings[0].Protocol != "TCP" {
		t.Fatalf("config = %+v", config)
	}
}
