package api

type InboundManagement struct {
	inbounds *[]Inbound
	save     func() error
}

func NewInboundManagement(inbounds *[]Inbound, save func() error) InboundManagement {
	if save == nil {
		save = func() error { return nil }
	}
	return InboundManagement{inbounds: inbounds, save: save}
}

func (m InboundManagement) List() []Inbound {
	if m.inbounds == nil {
		return nil
	}
	return NewInboundCatalog(*m.inbounds).List()
}

func (m InboundManagement) Get(name string) (Inbound, bool) {
	if m.inbounds == nil {
		return Inbound{}, false
	}
	return NewInboundCatalog(*m.inbounds).Get(name)
}

func (m InboundManagement) Create(inbound Inbound) (Inbound, error) {
	catalog := NewInboundCatalog(m.List())
	created, next, err := catalog.Create(inbound)
	if err != nil {
		return Inbound{}, err
	}
	if err := m.replaceAndSave(next.List()); err != nil {
		return Inbound{}, err
	}
	return created, nil
}

func (m InboundManagement) Update(name string, update Inbound) (Inbound, error) {
	catalog := NewInboundCatalog(m.List())
	updated, next, err := catalog.Update(name, update)
	if err != nil {
		return Inbound{}, err
	}
	if err := m.replaceAndSave(next.List()); err != nil {
		return Inbound{}, err
	}
	return updated, nil
}

func (m InboundManagement) Delete(name string) error {
	catalog := NewInboundCatalog(m.List())
	next, err := catalog.Delete(name)
	if err != nil {
		return err
	}
	return m.replaceAndSave(next.List())
}

func (m InboundManagement) replaceAndSave(next []Inbound) error {
	if m.inbounds != nil {
		*m.inbounds = next
	}
	return m.save()
}
