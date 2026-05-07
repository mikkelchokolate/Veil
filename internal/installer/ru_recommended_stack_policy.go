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
	case StackPanel:
		return RURecommendedStackPolicy{Stack: StackPanel}, nil
	case StackMieru:
		return RURecommendedStackPolicy{Stack: StackMieru, InstallMieru: true}, nil
	case "", StackBoth:
		return RURecommendedStackPolicy{Stack: StackBoth, InstallNaive: true, InstallHysteria2: true}, nil
	case StackNaive:
		return RURecommendedStackPolicy{Stack: StackNaive, InstallNaive: true}, nil
	case StackHysteria2:
		return RURecommendedStackPolicy{Stack: StackHysteria2, InstallHysteria2: true}, nil
	default:
		return RURecommendedStackPolicy{}, fmt.Errorf("unsupported stack %q", stack)
	}
}

func normalizeStack(stack Stack) (normalized Stack, installNaive bool, installHysteria2 bool, err error) {
	policy, err := NewRURecommendedStackPolicy(stack)
	if err != nil {
		return "", false, false, err
	}
	return policy.Stack, policy.InstallNaive, policy.InstallHysteria2, nil
}
