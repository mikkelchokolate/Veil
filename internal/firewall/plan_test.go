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

// The panel port is opened only when the panel is publicly reachable: a
// local (loopback) panel must not punch an external allow, while direct and
// caddy access still open it.
func TestUFWPlanPanelAccessGatesPanelPort(t *testing.T) {
	if plan := UFWPlan(Config{PanelAccess: "local", PanelPort: 2096}); len(plan) != 0 {
		t.Fatalf("local panel access must not open the panel port, got %#v", plan)
	}
	for _, access := range []string{"direct", "caddy", ""} {
		plan := UFWPlan(Config{PanelAccess: access, PanelPort: 2096})
		want := []Rule{{Command: "ufw", Args: []string{"allow", "2096/tcp", "comment", "Veil panel"}}}
		if !reflect.DeepEqual(plan, want) {
			t.Fatalf("PanelAccess=%q: got %#v, want panel rule %#v", access, plan, want)
		}
	}
}

func TestUFWPlanLEIPCertPort(t *testing.T) {
	plan := UFWPlan(Config{LEIPCertPort: 80})
	want := []Rule{{Command: "ufw", Args: []string{"allow", "80/tcp", "comment", "Veil ACME HTTP-01"}}}
	if !reflect.DeepEqual(plan, want) {
		t.Fatalf("unexpected plan:\n got: %#v\nwant: %#v", plan, want)
	}
}

func TestUFWPlanStagesSSHBeforePanelPorts(t *testing.T) {
	plan := UFWPlan(Config{PanelPort: 2096, SSHPorts: []int{2222, 22}})
	want := []Rule{
		{Command: "ufw", Args: []string{"allow", "2222/tcp", "comment", "Veil management SSH"}},
		{Command: "ufw", Args: []string{"allow", "22/tcp", "comment", "Veil management SSH"}},
		{Command: "ufw", Args: []string{"allow", "2096/tcp", "comment", "Veil panel"}},
	}
	if !reflect.DeepEqual(plan, want) {
		t.Fatalf("unexpected plan:\n got: %#v\nwant: %#v", plan, want)
	}
}

func TestUFWPlanOmitsSSHWhenNoPublicPorts(t *testing.T) {
	plan := UFWPlan(Config{SSHPorts: []int{22}})
	if len(plan) != 0 {
		t.Fatalf("SSH ports are only staged alongside real public openings, got %#v", plan)
	}
}
