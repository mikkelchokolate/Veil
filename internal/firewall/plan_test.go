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

func TestUFWPlanPanelPortWhenSharedPortZero(t *testing.T) {
	// When SharedPort is 0 (invalid) but PanelPort is valid, the plan
	// should still include the panel port rule — returning nil is a bug.
	plan := UFWPlan(Config{SharedPort: 0, PanelPort: 2096})
	if len(plan) != 1 {
		t.Fatalf("expected 1 panel rule when SharedPort=0, got %d rules: %#v", len(plan), plan)
	}
	want := Rule{Command: "ufw", Args: []string{"allow", "2096/tcp", "comment", "Veil panel"}}
	if plan[0].Command != want.Command || len(plan[0].Args) != len(want.Args) {
		t.Fatalf("unexpected rule:\n got: %#v\nwant: %#v", plan[0], want)
	}
	for i, arg := range want.Args {
		if plan[0].Args[i] != arg {
			t.Fatalf("unexpected rule:\n got: %#v\nwant: %#v", plan[0], want)
		}
	}
}
