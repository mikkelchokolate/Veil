package api

import (
	"encoding/json"
	"testing"
)

func TestBuildClientLinksAggregatesMieruTransportBindingsForClientProfile(t *testing.T) {
	response, err := BuildClientLinks(Settings{Domain: "vpn.example.com"}, []Inbound{
		{Name: "mieru-tcp", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Profiles: []ClientProfile{{Name: "alice", Password: "alice-pass", Enabled: true}}},
		{Name: "mieru-udp", Protocol: "mieru", Transport: "udp", Port: 443, Enabled: true, Profiles: []ClientProfile{{Name: "alice", Password: "alice-pass", Enabled: true}}},
	})
	if err != nil {
		t.Fatalf("BuildClientLinks: %v", err)
	}
	if response.Count != 1 || len(response.Artifacts) != 1 {
		t.Fatalf("Mieru TCP/UDP profile should be delivered as one client config: %+v", response)
	}
	var config struct {
		ProfileName string `json:"profileName"`
		Servers     []struct {
			PortBindings []struct {
				Port     int    `json:"port"`
				Protocol string `json:"protocol"`
			} `json:"portBindings"`
		} `json:"servers"`
	}
	if err := json.Unmarshal([]byte(response.Links[0].Config), &config); err != nil {
		t.Fatalf("invalid Mieru client config: %v\n%s", err, response.Links[0].Config)
	}
	if config.ProfileName != "mieru/alice" || len(config.Servers) != 1 || len(config.Servers[0].PortBindings) != 2 {
		t.Fatalf("config = %+v", config)
	}
	if config.Servers[0].PortBindings[0].Protocol != "TCP" || config.Servers[0].PortBindings[1].Protocol != "UDP" {
		t.Fatalf("port bindings = %+v", config.Servers[0].PortBindings)
	}
}
