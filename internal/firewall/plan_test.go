package firewall

import (
	"reflect"
	"testing"
)

func TestFirewallConfigDoesNotExposeSharedProxyPortPlanning(t *testing.T) {
	configType := reflect.TypeOf(Config{})
	for _, field := range []string{"SharedPort", "EnableTCP", "EnableUDP"} {
		if _, ok := configType.FieldByName(field); ok {
			t.Fatalf("firewall Config should not expose legacy shared proxy port planning field %s", field)
		}
	}
}

func TestUFWPlanPanelPort(t *testing.T) {
	plan := UFWPlan(Config{PanelPort: 2096})
	want := []Rule{{Command: "ufw", Args: []string{"allow", "2096/tcp", "comment", "Veil panel"}}}
	if !reflect.DeepEqual(plan, want) {
		t.Fatalf("unexpected plan:\n got: %#v\nwant: %#v", plan, want)
	}
}

func TestUFWPlanPanelHTTPSPort(t *testing.T) {
	plan := UFWPlan(Config{PanelHTTPSPort: 443})
	want := []Rule{{Command: "ufw", Args: []string{"allow", "443/tcp", "comment", "Veil panel HTTPS"}}}
	if !reflect.DeepEqual(plan, want) {
		t.Fatalf("unexpected plan:\n got: %#v\nwant: %#v", plan, want)
	}
}

func TestUFWPlanOmitsZeroPorts(t *testing.T) {
	if plan := UFWPlan(Config{}); len(plan) != 0 {
		t.Fatalf("expected no rules for zero ports, got %#v", plan)
	}
}
