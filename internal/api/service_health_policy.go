package api

import (
	"errors"
	"fmt"
)

type ServiceHealthPolicy struct{}

func NewServiceHealthPolicy() ServiceHealthPolicy { return ServiceHealthPolicy{} }

func (ServiceHealthPolicy) RequireHealthy(checks []ServiceHealthResult) error {
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

func requireHealthyServices(checks []ServiceHealthResult) error {
	return NewServiceHealthPolicy().RequireHealthy(checks)
}
