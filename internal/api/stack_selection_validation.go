package api

import "fmt"

type StackSelectionValidation struct{}

func NewStackSelectionValidation() StackSelectionValidation { return StackSelectionValidation{} }

func (StackSelectionValidation) Validate(stack string) error {
	switch stack {
	case "panel", "mieru", "both", "naive", "hysteria2":
		return nil
	default:
		return fmt.Errorf("unsupported stack: %s", stack)
	}
}
