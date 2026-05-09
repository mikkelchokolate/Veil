package api

import "github.com/veil-panel/veil/internal/service"

type PromotedServiceActionCatalog struct {
	inner service.PromotedServiceActionCatalog
}

func NewPromotedServiceActionCatalog(applyRoot string) PromotedServiceActionCatalog {
	return PromotedServiceActionCatalog{inner: service.NewPromotedServiceActionCatalog(applyRoot, NewManagedRuntimeCatalog())}
}

func (c PromotedServiceActionCatalog) Commands(liveFiles []string) [][]string {
	return c.inner.Commands(liveFiles)
}
