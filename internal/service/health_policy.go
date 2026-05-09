package service

import (
	"errors"
	"fmt"

	"github.com/veil-panel/veil/internal/model"
)

type HealthPolicy struct{}

func NewHealthPolicy() HealthPolicy { return HealthPolicy{} }

func (HealthPolicy) RequireHealthy(checks []model.ServiceHealthResult) error {
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
