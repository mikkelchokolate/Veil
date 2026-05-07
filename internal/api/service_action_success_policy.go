package api

import (
	"errors"
	"fmt"
)

type ServiceActionSuccessPolicy struct{}

func NewServiceActionSuccessPolicy() ServiceActionSuccessPolicy { return ServiceActionSuccessPolicy{} }

func (ServiceActionSuccessPolicy) RequireSuccessful(actions []ServiceActionResult) error {
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

func requireSuccessfulServiceActions(actions []ServiceActionResult) error {
	return NewServiceActionSuccessPolicy().RequireSuccessful(actions)
}
