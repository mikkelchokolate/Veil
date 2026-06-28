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
	}
	if err := json.Unmarshal([]byte(response.Links[0].Config), &config); err != nil {
		t.Fatalf("invalid Mieru client config: %v\n%s", err, response.Links[0].Config)
	}
	p := config.Profiles[0]
	if config.ActiveProfile != "mieru/alice" || p.ProfileName != "mieru/alice" || p.User.Name != "alice" || p.User.Password != "alice-pass" || len(p.Servers) != 1 || p.Servers[0].DomainName != "vpn.example.com" || p.Servers[0].PortBindings[0].Protocol != "TCP" {
		t.Fatalf("config = %+v", config)
	}
}
