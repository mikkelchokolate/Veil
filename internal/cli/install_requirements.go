package cli

import (
	"fmt"

	"github.com/veil-panel/veil/internal/installer"
)

type RURecommendedInstallRequirements struct {
	stack string
}

func NewRURecommendedInstallRequirements(stack string) RURecommendedInstallRequirements {
	return RURecommendedInstallRequirements{stack: stack}
}

func (r RURecommendedInstallRequirements) Validate(opts ruRecommendedInstallOptions) error {
	policy, err := installer.NewRURecommendedStackPolicy(installer.Stack(r.stack))
	if err != nil {
		return err
	}
	if policy.RequiresDomain() {
		if opts.Domain == "" {
			return fmt.Errorf("--domain is required for ru-recommended profile")
		}
		if opts.Email == "" {
			return fmt.Errorf("--email is required for ru-recommended profile")
		}
	}
	if policy.RequiresSharedProxyPort() && (opts.SharedPort <= 0 || opts.SharedPort > 65535) {
		return fmt.Errorf("--port is required and must be between 1 and 65535")
	}
	return nil
}
