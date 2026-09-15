package clientaccess

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildClientLinksMieruIgnoresInboundDomain(t *testing.T) {
	response, err := BuildClientLinks(Settings{}, []Inbound{
		{Name: "mieru-tcp", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Password: "mieru-pass", ProtocolFields: map[string]any{"domain": "inbound.example.com"}},
	})
	if err != nil {
		t.Fatalf("BuildClientLinks: %v", err)
	}
	if response.Count != 0 {
		t.Fatalf("expected no mieru links when only inbound domain is set, got %+v", response)
	}
}

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
	}
	if err := json.Unmarshal([]byte(response.Links[0].Config), &config); err != nil {
		t.Fatalf("invalid Mieru client config: %v\n%s", err, response.Links[0].Config)
	}
	if config.ActiveProfile != "mieru/alice" || len(config.Profiles) != 1 {
		t.Fatalf("config = %+v", config)
	}
	p := config.Profiles[0]
	if p.ProfileName != "mieru/alice" || len(p.Servers) != 1 || len(p.Servers[0].PortBindings) != 2 {
		t.Fatalf("profile = %+v", p)
	}
	if p.Servers[0].PortBindings[0].Protocol != "TCP" || p.Servers[0].PortBindings[1].Protocol != "UDP" {
		t.Fatalf("port bindings = %+v", p.Servers[0].PortBindings)
	}
	uri := response.Links[0].URI
	if !strings.Contains(uri, "port=443") {
		t.Fatalf("aggregated URI missing first port: %q", uri)
	}
	ports := strings.Count(uri, "port=443")
	if ports != 2 {
		t.Fatalf("aggregated URI must repeat port for both bindings, got %d in %q", ports, uri)
	}
	if !strings.Contains(uri, "protocol=TCP") || !strings.Contains(uri, "protocol=UDP") {
		t.Fatalf("aggregated URI must include both protocols: %q", uri)
	}
	payload := NewClientSubscriptionPayload(response).Build()
	if !strings.Contains(payload, "port=443") || !strings.Contains(payload, "protocol=TCP") || !strings.Contains(payload, "protocol=UDP") {
		t.Fatalf("subscription payload missing aggregated port/protocol pairs: %q", payload)
	}
}
