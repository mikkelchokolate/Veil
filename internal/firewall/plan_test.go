package firewall

import (
	"reflect"
	"testing"
)

func TestUFWPlanForSharedPort(t *testing.T) {
	plan := UFWPlan(Config{SharedPort: 443, PanelPort: 2096})
	want := []Rule{
		{Command: "ufw", Args: []string{"allow", "443/tcp", "comment", "Veil NaiveProxy"}},
		{Command: "ufw", Args: []string{"allow", "443/udp", "comment", "Veil Hysteria2"}},
		{Command: "ufw", Args: []string{"allow", "2096/tcp", "comment", "Veil panel"}},
	}
	if !reflect.DeepEqual(plan, want) {
		t.Fatalf("unexpected plan:\n got: %#v\nwant: %#v", plan, want)
	}
}

func TestUFWPlanCanOmitUnselectedProxyTransports(t *testing.T) {
	plan := UFWPlan(Config{SharedPort: 443, PanelPort: 2096, EnableTCP: false, EnableUDP: true})
	want := []Rule{
		{Command: "ufw", Args: []string{"allow", "443/udp", "comment", "Veil Hysteria2"}},
		{Command: "ufw", Args: []string{"allow", "2096/tcp", "comment", "Veil panel"}},
	}
	if !reflect.DeepEqual(plan, want) {
		t.Fatalf("unexpected plan:\n got: %#v\nwant: %#v", plan, want)
	}
}

func TestUFWPlanOmitsPanelWhenZero(t *testing.T) {
	plan := UFWPlan(Config{SharedPort: 8443})
	if len(plan) != 2 {
		t.Fatalf("expected 2 rules, got %#v", plan)
	}
}
