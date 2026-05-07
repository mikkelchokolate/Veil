package api

import (
	"errors"
	"fmt"
)

type ConfigValidationPassPolicy struct{}

func NewConfigValidationPassPolicy() ConfigValidationPassPolicy { return ConfigValidationPassPolicy{} }

func (ConfigValidationPassPolicy) RequirePassed(validations []ConfigValidationResult) error {
	for _, validation := range validations {
		if validation.Skipped || !validation.Valid {
			if validation.Error != "" {
				return errors.New(validation.Error)
			}
			return fmt.Errorf("%s validation did not pass", validation.Name)
		}
	}
	return nil
}

func requirePassedValidations(validations []ConfigValidationResult) error {
	return NewConfigValidationPassPolicy().RequirePassed(validations)
}
