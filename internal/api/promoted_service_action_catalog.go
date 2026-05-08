package api

type PromotedServiceActionCatalog struct {
	applyRoot string
}

func NewPromotedServiceActionCatalog(applyRoot string) PromotedServiceActionCatalog {
	return PromotedServiceActionCatalog{applyRoot: applyRoot}
}

func (c PromotedServiceActionCatalog) Commands(liveFiles []string) [][]string {
	return NewManagedRuntimeCatalog().PromotedCommands(c.applyRoot, liveFiles)
}
