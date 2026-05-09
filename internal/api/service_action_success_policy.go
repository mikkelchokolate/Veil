package api

import "github.com/veil-panel/veil/internal/service"

type ServiceActionSuccessPolicy = service.ActionSuccessPolicy

func NewServiceActionSuccessPolicy() ServiceActionSuccessPolicy {
	return service.NewActionSuccessPolicy()
}

func requireSuccessfulServiceActions(actions []ServiceActionResult) error {
	return service.NewActionSuccessPolicy().RequireSuccessful(actions)
}
