package protocols

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProtocolInfoJSONUsesPanelContractNames(t *testing.T) {
	info := ProtocolInfo{
		Metadata: Metadata{
			Protocol:        "demo",
			DisplayName:     "Demo",
			Transports:      []string{"tcp"},
			RequiresCaddy:   true,
			FirewallService: "Demo Service",
			MaxEnabled:      2,
		},
	}
	body, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal protocol info: %v", err)
	}
	encoded := string(body)
	for _, want := range []string{
		`"protocol":"demo"`,
		`"displayName":"Demo"`,
		`"transports":["tcp"]`,
		`"requiresCaddy":true`,
		`"firewallService":"Demo Service"`,
		`"maxEnabled":2`,
		`"inboundFieldSchema"`,
		`"settingsFieldSchema"`,
	} {
		if !strings.Contains(encoded, want) {
			t.Fatalf("protocol info JSON %s missing %s", encoded, want)
		}
	}
	for _, legacy := range []string{`"Protocol"`, `"DisplayName"`, `"Transports"`} {
		if strings.Contains(encoded, legacy) {
			t.Fatalf("protocol info JSON must not expose legacy field %s: %s", legacy, encoded)
		}
	}
}
