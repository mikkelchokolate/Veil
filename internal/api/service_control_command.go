package api

type ServiceControlCommand struct {
	catalog ManagedRuntimeCatalog
}

func NewServiceControlCommand() ServiceControlCommand {
	return ServiceControlCommand{catalog: NewManagedRuntimeCatalog()}
}

func (c ServiceControlCommand) Build(name, action string) ([]string, bool) {
	return c.catalog.ServiceActionCommand(name, action)
}

func (c ServiceControlCommand) Allows(name string) bool {
	return c.catalog.AllowsActionName(name)
}
