package applyflow

import (
	"errors"
	"fmt"

	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/model"
)

// ConfigValidationPassPolicy gates live apply on the staged-config
// validation results. It delegates to generatedconfig.ValidationPassPolicy —
// the single fail-closed contract: protocols without a standalone checker
// produce no validation entry at all, so a Skipped entry always means a
// CONFIGURED validator could not run (binary missing from PATH, empty
// command). That must block promotion — otherwise a bad config goes live
// whenever PATH lacks the checker, and the post-restart health probe is not
// a substitute for stage-time validation (issue #686).
type ConfigValidationPassPolicy struct {
	inner generatedconfig.ValidationPassPolicy
}

func NewConfigValidationPassPolicy() ConfigValidationPassPolicy {
	return ConfigValidationPassPolicy{inner: generatedconfig.NewValidationPassPolicy()}
}

func (p ConfigValidationPassPolicy) RequirePassed(validations []model.ConfigValidationResult) error {
	return p.inner.RequirePassed(validations)
}

type ServiceActionSuccessPolicy struct{}

func NewServiceActionSuccessPolicy() ServiceActionSuccessPolicy { return ServiceActionSuccessPolicy{} }

func (ServiceActionSuccessPolicy) RequireSuccessful(actions []model.ServiceActionResult) error {
	for _, action := range actions {
		if !action.Success {
			if action.Error != "" {
				return errors.New(action.Error)
			}
			return fmt.Errorf("%s service action failed", action.Name)
		}
	}
	return nil
}

type ServiceHealthPolicy struct{}

func NewServiceHealthPolicy() ServiceHealthPolicy { return ServiceHealthPolicy{} }

func (ServiceHealthPolicy) RequireHealthy(checks []model.ServiceHealthResult) error {
	for _, check := range checks {
		if !check.Healthy {
			if check.Error != "" {
				return errors.New(check.Error)
			}
			return fmt.Errorf("%s health check failed", check.Name)
		}
	}
	return nil
}
