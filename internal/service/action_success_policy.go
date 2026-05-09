package service

import (
	"errors"
	"fmt"

	"github.com/veil-panel/veil/internal/model"
)

type ActionSuccessPolicy struct{}

func NewActionSuccessPolicy() ActionSuccessPolicy { return ActionSuccessPolicy{} }

func (ActionSuccessPolicy) RequireSuccessful(actions []model.ServiceActionResult) error {
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
