package clientaccess

import (
	"encoding/json"
	"testing"
)

// TestMieruFallbackExportPreservesPasswordBytes covers audit #311: the
// exported client config must carry the same fallback credential bytes the
// server renders — no trimming on only one side.
func TestMieruFallbackExportPreservesPasswordBytes(t *testing.T) {
	response, err := BuildClientLinks(Settings{Domain: "vpn.example.com"}, []Inbound{{
		Name: "mieru", Protocol: "mieru", Transport: "udp", Port: 443, Enabled: true,
		Password: " Fixture-Password-123 ",
	}})
	if err != nil {
		t.Fatalf("BuildClientLinks: %v", err)
	}
	if response.Count != 1 {
		t.Fatalf("response = %+v", response)
	}
	var config struct {
		Profiles []struct {
			User struct {
				Password string `json:"password"`
			} `json:"user"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal([]byte(response.Links[0].Config), &config); err != nil {
		t.Fatalf("invalid config: %v\n%s", err, response.Links[0].Config)
	}
	if len(config.Profiles) != 1 || config.Profiles[0].User.Password != " Fixture-Password-123 " {
		t.Fatalf("exported config = %+v, want byte-for-byte fallback password", config.Profiles)
	}
}

// TestMieruFallbackExportUsesDynamicPasswordBytes ensures a dynamic-only
// protocolFields password reaches the export with identical bytes (audit #311).
func TestMieruFallbackExportUsesDynamicPasswordBytes(t *testing.T) {
	response, err := BuildClientLinks(Settings{Domain: "vpn.example.com"}, []Inbound{{
		Name: "mieru", Protocol: "mieru", Transport: "udp", Port: 443, Enabled: true,
		ProtocolFields: map[string]any{"password": " dyn "},
	}})
	if err != nil {
		t.Fatalf("BuildClientLinks: %v", err)
	}
	if response.Count != 1 {
		t.Fatalf("response = %+v", response)
	}
	var config struct {
		Profiles []struct {
			User struct {
				Password string `json:"password"`
			} `json:"user"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal([]byte(response.Links[0].Config), &config); err != nil {
		t.Fatalf("invalid config: %v\n%s", err, response.Links[0].Config)
	}
	if len(config.Profiles) != 1 || config.Profiles[0].User.Password != " dyn " {
		t.Fatalf("exported config = %+v, want dynamic password bytes", config.Profiles)
	}
}
