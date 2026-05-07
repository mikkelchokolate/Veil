package api

import (
	"errors"
)

var (
	ErrInboundInvalid                = errors.New("inbound invalid")
	ErrInboundNotFound               = errors.New("inbound not found")
	ErrInboundDuplicateName          = errors.New("inbound name already exists")
	ErrInboundDuplicateTransportPort = errors.New("inbound transport/port already exists")
)

type InboundPasswordGenerator func() string

type InboundCatalog struct {
	inbounds         []Inbound
	passwordGenerate InboundPasswordGenerator
}

func NewInboundCatalog(inbounds []Inbound) InboundCatalog {
	return NewInboundCatalogWithPasswordGenerator(inbounds, generateInboundPassword)
}

func NewInboundCatalogWithPasswordGenerator(inbounds []Inbound, generator InboundPasswordGenerator) InboundCatalog {
	if generator == nil {
		generator = generateInboundPassword
	}
	return InboundCatalog{inbounds: cloneInbounds(inbounds), passwordGenerate: generator}
}

func (c InboundCatalog) List() []Inbound {
	return cloneInbounds(c.inbounds)
}

func (c InboundCatalog) Get(name string) (Inbound, bool) {
	idx := c.index(name)
	if idx < 0 {
		return Inbound{}, false
	}
	return c.inbounds[idx], true
}

func (c InboundCatalog) Create(inbound Inbound) (Inbound, InboundCatalog, error) {
	if err := validateInboundForCreate(inbound); err != nil {
		return Inbound{}, c, err
	}
	if c.index(inbound.Name) >= 0 {
		return Inbound{}, c, ErrInboundDuplicateName
	}
	if c.hasTransportPort(inbound.Transport, inbound.Port, -1) {
		return Inbound{}, c, ErrInboundDuplicateTransportPort
	}
	NewInboundPasswordPolicy(c.passwordGenerate).ApplyCreate(&inbound)
	c.fillMissingProfilePasswords(&inbound, nil)
	next := NewInboundCatalogWithPasswordGenerator(append(c.List(), inbound), c.passwordGenerate)
	return inbound, next, nil
}

func (c InboundCatalog) Update(name string, update Inbound) (Inbound, InboundCatalog, error) {
	idx := c.index(name)
	if idx < 0 {
		return Inbound{}, c, ErrInboundNotFound
	}
	if err := validateInboundForUpdate(update); err != nil {
		return Inbound{}, c, err
	}
	if c.hasTransportPort(update.Transport, update.Port, idx) {
		return Inbound{}, c, ErrInboundDuplicateTransportPort
	}
	update.Name = name
	NewInboundPasswordPolicy(c.passwordGenerate).ApplyUpdate(&update, c.inbounds[idx])
	c.fillMissingProfilePasswords(&update, c.inbounds[idx].Profiles)
	nextInbounds := c.List()
	nextInbounds[idx] = update
	return update, NewInboundCatalogWithPasswordGenerator(nextInbounds, c.passwordGenerate), nil
}

func (c InboundCatalog) Delete(name string) (InboundCatalog, error) {
	idx := c.index(name)
	if idx < 0 {
		return c, ErrInboundNotFound
	}
	next := c.List()
	next = append(next[:idx], next[idx+1:]...)
	return NewInboundCatalogWithPasswordGenerator(next, c.passwordGenerate), nil
}

func (c InboundCatalog) fillMissingProfilePasswords(inbound *Inbound, previous []ClientProfile) {
	inbound.Profiles = NewClientProfileCatalogWithPasswordGenerator(inbound.Profiles, c.passwordGenerate).WithCompletedPasswords(previous)
}

func (c InboundCatalog) index(name string) int {
	for idx, inbound := range c.inbounds {
		if inbound.Name == name {
			return idx
		}
	}
	return -1
}

func (c InboundCatalog) hasTransportPort(transport string, port int, exceptIndex int) bool {
	return NewInboundTransportPortIndex(c.inbounds).Has(transport, port, exceptIndex)
}
