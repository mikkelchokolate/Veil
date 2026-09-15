package clientaccess

import (
	"encoding/json"
	"testing"
)

func TestBuildClientLinksOmitsMieruFallbackWhenAllProfilesDisabled(t *testing.T) {
	response, err := BuildClientLinks(Settings{Domain: "vpn.example.com"}, []Inbound{{
		Name: "mieru", Protocol: "mieru", Transport: "udp", Port: 443, Enabled: true, Password: "inbound-pass",
		Profiles: []ClientProfile{{Name: "alice", Username: "alice", Password: "alice-pass", Enabled: false}},
	}})
	if err != nil {
		t.Fatalf("BuildClientLinks: %v", err)
	}
	if response.Count != 0 || len(response.Links) != 0 {
		t.Fatalf("all-disabled Mieru profiles must not export fallback, got %+v", response)
	}
}

func TestBuildClientLinksIncludesMieruClientConfigForInboundPasswordFallback(t *testing.T) {
	response, err := BuildClientLinks(Settings{Domain: "vpn.example.com"}, []Inbound{{Name: "mieru", Protocol: "mieru", Transport: "udp", Port: 443, Enabled: true, Password: "inbound-pass"}})
	if err != nil {
		t.Fatalf("BuildClientLinks: %v", err)
	}
	if response.Count != 1 || response.Links[0].Name != "mieru" || response.Links[0].Config == "" {
		t.Fatalf("response = %+v", response)
	}
	var config struct {
		ActiveProfile string `json:"activeProfile"`
		Profiles      []struct {
			User struct {
				Name     string `json:"name"`
				Password string `json:"password"`
			} `json:"user"`
			Servers []struct {
				PortBindings []struct {
					Protocol string `json:"protocol"`
				} `json:"portBindings"`
			} `json:"servers"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal([]byte(response.Links[0].Config), &config); err != nil {
		t.Fatalf("invalid config: %v\n%s", err, response.Links[0].Config)
	}
	p := config.Profiles[0]
	if config.ActiveProfile != "mieru" || p.User.Name != "mieru" || p.User.Password != "inbound-pass" || p.Servers[0].PortBindings[0].Protocol != "UDP" {
		t.Fatalf("config = %+v", config)
	}
}
