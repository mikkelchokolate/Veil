package installer

func hasFirewallAction(plan InstallPlan, portProtocol string) bool {
	for _, action := range plan.FirewallActions {
		if len(action.Args) >= 2 && action.Args[1] == portProtocol {
			return true
		}
	}
	return false
}
