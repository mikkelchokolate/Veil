package api

import "fmt"

type StackSelectionValidation struct{}

func NewStackSelectionValidation() StackSelectionValidation { return StackSelectionValidation{} }

func (StackSelectionValidation) Validate(stack string) error {
	if NewStackSelectionCatalog().Supports(stack) {
		return nil
	}
	return fmt.Errorf("unsupported stack: %s", stack)
}
