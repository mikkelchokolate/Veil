package applyflow

import (
	"errors"
	"fmt"

	"github.com/mikkelchokolate/Veil/internal/model"
)

type ConfigValidationPassPolicy struct{}

func NewConfigValidationPassPolicy() ConfigValidationPassPolicy { return ConfigValidationPassPolicy{} }

func (ConfigValidationPassPolicy) RequirePassed(validations []model.ConfigValidationResult) error {
	for _, validation := range validations {
		// A skipped validation (the validator binary is absent or the protocol has
		// no standalone checker) must not block the apply — the post-restart
		// service health check is the real gate and rolls back on failure.
		if validation.Skipped {
			continue
		}
		if !validation.Valid {
			if validation.Error != "" {
				return errors.New(validation.Error)
			}
			return fmt.Errorf("%s validation did not pass", validation.Name)
		}
	}
	return nil
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
