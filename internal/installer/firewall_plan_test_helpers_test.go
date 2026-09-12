package installer

import "strings"

func hasFirewallAction(plan InstallPlan, portProtocol string) bool {
	for _, action := range plan.FirewallActions {
		if len(action.Args) >= 2 && action.Args[1] == portProtocol {
			return true
		}
	}
	return false
}

func hasSSHFirewallAction(plan InstallPlan) bool {
	for _, action := range plan.FirewallActions {
		if strings.Contains(strings.ToLower(strings.Join(action.Args, " ")), "ssh") {
			return true
		}
	}
	return false
}
