package api

type InboundManagement struct {
	mutation ManagementStateMutation
}

func NewInboundManagement(inbounds *[]Inbound, save func() error) InboundManagement {
	return InboundManagement{mutation: NewManagementStateMutation(ManagementStateMutationTarget{Inbounds: inbounds}, save)}
}

func (m InboundManagement) List() []Inbound {
	return m.mutation.Inbounds()
}

func (m InboundManagement) Get(name string) (Inbound, bool) {
	return m.mutation.Inbound(name)
}

func (m InboundManagement) Create(inbound Inbound) (Inbound, error) {
	return m.mutation.CreateInbound(inbound)
}

func (m InboundManagement) Update(name string, update Inbound) (Inbound, error) {
	return m.mutation.UpdateInbound(name, update)
}

func (m InboundManagement) Delete(name string) error {
	return m.mutation.DeleteInbound(name)
}
