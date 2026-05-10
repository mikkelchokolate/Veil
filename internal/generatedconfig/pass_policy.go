package generatedconfig

import (
	"errors"
	"fmt"
)

type ValidationPassPolicy struct{}

func NewValidationPassPolicy() ValidationPassPolicy { return ValidationPassPolicy{} }

func (ValidationPassPolicy) RequirePassed(validations []ConfigValidationResult) error {
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
