package api

import "github.com/veil-panel/veil/internal/service"

type PromotedServiceReloader struct {
	inner service.PromotedServiceReloader
}

func NewPromotedServiceReloader(applyRoot string, run func([]string) ServiceActionResult) PromotedServiceReloader {
	if run == nil {
		run = serviceActionRunner
	}
	return PromotedServiceReloader{inner: service.NewPromotedServiceReloader(applyRoot, NewManagedRuntimeCatalog(), func(command []string) ServiceActionResult {
		return run(command)
	})}
}

func (r PromotedServiceReloader) Reload(liveFiles []string) []ServiceActionResult {
	return r.inner.Reload(liveFiles)
}

func containsPath(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
}
