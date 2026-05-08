package installer

import (
	"fmt"
	"strings"
)

type RURecommendedStackPolicy struct {
	Stack            Stack
	InstallNaive     bool
	InstallHysteria2 bool
	InstallMieru     bool
}

func NewRURecommendedStackPolicy(stack Stack) (RURecommendedStackPolicy, error) {
	switch Stack(strings.TrimSpace(string(stack))) {
	case "", StackPanel, StackMieru, StackBoth, StackNaive, StackHysteria2:
		return RURecommendedStackPolicy{Stack: StackPanel}, nil
	default:
		return RURecommendedStackPolicy{}, fmt.Errorf("unsupported stack %q", stack)
	}
}

func (p RURecommendedStackPolicy) RequiresDomain() bool {
	return p.InstallNaive || p.InstallHysteria2
}

func (p RURecommendedStackPolicy) RequiresSharedProxyPort() bool {
	return p.InstallNaive || p.InstallHysteria2
}

func normalizeStack(stack Stack) (normalized Stack, installNaive bool, installHysteria2 bool, err error) {
	policy, err := NewRURecommendedStackPolicy(stack)
	if err != nil {
		return "", false, false, err
	}
	return policy.Stack, policy.InstallNaive, policy.InstallHysteria2, nil
}
