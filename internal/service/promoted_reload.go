package service

import "github.com/mikkelchokolate/Veil/internal/model"

type ServiceActionRunner func([]string) model.ServiceActionResult

type PromotedServiceActionCatalog struct {
	applyRoot string
	catalog   ManagedRuntimeCatalog
}

func NewPromotedServiceActionCatalog(applyRoot string, catalog ManagedRuntimeCatalog) PromotedServiceActionCatalog {
	return PromotedServiceActionCatalog{applyRoot: applyRoot, catalog: catalog}
}

func (c PromotedServiceActionCatalog) Commands(liveFiles []string) [][]string {
	return c.catalog.PromotedCommands(c.applyRoot, liveFiles)
}

type PromotedServiceReloader struct {
	applyRoot string
	catalog   ManagedRuntimeCatalog
	run       ServiceActionRunner
}

func NewPromotedServiceReloader(applyRoot string, catalog ManagedRuntimeCatalog, run ServiceActionRunner) PromotedServiceReloader {
	return PromotedServiceReloader{applyRoot: applyRoot, catalog: catalog, run: run}
}

func (r PromotedServiceReloader) Reload(liveFiles []string) []model.ServiceActionResult {
	commands := NewPromotedServiceActionCatalog(r.applyRoot, r.catalog).Commands(liveFiles)
	results := make([]model.ServiceActionResult, 0, len(commands))
	for _, command := range commands {
		result := r.run(command)
		if result.Name == "" && len(command) > 0 {
			result.Name = command[len(command)-1]
		}
		if result.Command == nil {
			result.Command = append([]string(nil), command...)
		}
		results = append(results, result)
		if !result.Success {
			break
		}
	}
	return results
}

type ServiceHealthChecker func(name string) model.ServiceHealthResult

type ServiceHealthCollection struct {
	check ServiceHealthChecker
}

func NewServiceHealthCollection(check ServiceHealthChecker) ServiceHealthCollection {
	return ServiceHealthCollection{check: check}
}

func (c ServiceHealthCollection) Check(actions []model.ServiceActionResult) []model.ServiceHealthResult {
	checks := []model.ServiceHealthResult{}
	for _, action := range actions {
		if !action.Success || action.Name == "" {
			continue
		}
		checks = append(checks, c.check(action.Name))
	}
	return checks
}
