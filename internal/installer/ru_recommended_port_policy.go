package installer

type RURecommendedPortPolicy struct {
	availability PortAvailability
	randomPort   func() int
}

func NewRURecommendedPortPolicy(availability PortAvailability, randomPort func() int) RURecommendedPortPolicy {
	if randomPort == nil {
		randomPort = func() int { return 443 }
	}
	return RURecommendedPortPolicy{availability: availability, randomPort: randomPort}
}

func (p RURecommendedPortPolicy) Plan(explicitPort int, stack RURecommendedStackPolicy) (SharedPortPlan, error) {
	if !stack.RequiresSharedProxyPort() {
		return SharedPortPlan{}, nil
	}
	if explicitPort > 0 {
		return PlanExplicitStackPort(p.availability, explicitPort, stack.InstallNaive, stack.InstallHysteria2)
	}
	return PlanStackPort(p.availability, []int{443, 8443}, p.randomPort, stack.InstallNaive, stack.InstallHysteria2), nil
}
