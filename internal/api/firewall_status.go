package api

import "github.com/veil-panel/veil/internal/firewall"

var firewallStatusReader = readFirewallStatus

func readFirewallStatus() (bool, error) {
	return firewall.NewStatusReader(nil).Active()
}
