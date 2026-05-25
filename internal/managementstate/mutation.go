package managementstate

import (
	"github.com/veil-panel/veil/internal/inbounds"
	"github.com/veil-panel/veil/internal/model"
	"github.com/veil-panel/veil/internal/routing"
	veilsettings "github.com/veil-panel/veil/internal/settings"
	veilwarp "github.com/veil-panel/veil/internal/warp"
)

type Settings = model.Settings
type Inbound = model.Inbound
type RoutingRule = model.RoutingRule
type WarpConfig = model.WarpConfig

var (
	ErrRoutingRuleInvalid       = routing.ErrRoutingRuleInvalid
	ErrRoutingRuleNotFound      = routing.ErrRoutingRuleNotFound
	ErrRoutingRuleDuplicateName = routing.ErrRoutingRuleDuplicateName
)

type MutationTarget struct {
	Settings *Settings
	Inbounds *[]Inbound
	Rules    *[]RoutingRule
	Warp     *WarpConfig
}

type ManagementStateMutationTarget = MutationTarget

type Mutation struct {
	target MutationTarget
	save   func() error
}

type ManagementStateMutation = Mutation

func NewMutation(target MutationTarget, save func() error) Mutation {
	if save == nil {
		save = func() error { return nil }
	}
	return Mutation{target: target, save: save}
}

func NewManagementStateMutation(target ManagementStateMutationTarget, save func() error) ManagementStateMutation {
	return NewMutation(target, save)
}

func (m Mutation) Settings() Settings {
	if m.target.Settings == nil {
		return Settings{}
	}
	return veilsettings.NewSettingsRedaction().Redact(*m.target.Settings)
}

func (m Mutation) UpdateSettings(update Settings) (Settings, error) {
	current := Settings{}
	if m.target.Settings != nil {
		current = *m.target.Settings
	}
	if err := veilsettings.NewSettingsValidation().NormalizeAndValidate(&update, current); err != nil {
		return Settings{}, err
	}
	if m.target.Settings != nil {
		*m.target.Settings = update
	}
	if err := m.save(); err != nil {
		return Settings{}, err
	}
	return veilsettings.NewSettingsRedaction().Redact(update), nil
}

func (m Mutation) Inbounds() []Inbound {
	if m.target.Inbounds == nil {
		return nil
	}
	return inbounds.NewInboundCatalog(*m.target.Inbounds).List()
}

func (m Mutation) Inbound(name string) (Inbound, bool) {
	if m.target.Inbounds == nil {
		return Inbound{}, false
	}
	return inbounds.NewInboundCatalog(*m.target.Inbounds).Get(name)
}

func (m Mutation) CreateInbound(inbound Inbound) (Inbound, error) {
	catalog := inbounds.NewInboundCatalog(m.Inbounds())
	created, next, err := catalog.Create(inbound)
	if err != nil {
		return Inbound{}, err
	}
	if err := m.replaceInbounds(next.List()); err != nil {
		return Inbound{}, err
	}
	return created, nil
}

func (m Mutation) UpdateInbound(name string, update Inbound) (Inbound, error) {
	catalog := inbounds.NewInboundCatalog(m.Inbounds())
	updated, next, err := catalog.Update(name, update)
	if err != nil {
		return Inbound{}, err
	}
	if err := m.replaceInbounds(next.List()); err != nil {
		return Inbound{}, err
	}
	return updated, nil
}

func (m Mutation) DeleteInbound(name string) error {
	catalog := inbounds.NewInboundCatalog(m.Inbounds())
	next, err := catalog.Delete(name)
	if err != nil {
		return err
	}
	return m.replaceInbounds(next.List())
}

func (m Mutation) replaceInbounds(next []Inbound) error {
	if m.target.Inbounds != nil {
		*m.target.Inbounds = next
	}
	return m.save()
}

func (m Mutation) RoutingRules() []RoutingRule {
	if m.target.Rules == nil {
		return nil
	}
	return append([]RoutingRule(nil), (*m.target.Rules)...)
}

func (m Mutation) RoutingRule(name string) (RoutingRule, bool) {
	idx := m.routingRuleIndex(name)
	if idx < 0 || m.target.Rules == nil {
		return RoutingRule{}, false
	}
	return (*m.target.Rules)[idx], true
}

func (m Mutation) CreateRoutingRule(rule RoutingRule) (RoutingRule, error) {
	if err := routing.NewRoutingRuleValidation().ValidateCreate(rule); err != nil {
		return RoutingRule{}, err
	}
	if m.routingRuleIndex(rule.Name) >= 0 {
		return RoutingRule{}, ErrRoutingRuleDuplicateName
	}
	next := append(m.RoutingRules(), rule)
	if err := m.replaceRoutingRules(next); err != nil {
		return RoutingRule{}, err
	}
	return rule, nil
}

func (m Mutation) UpdateRoutingRule(name string, update RoutingRule) (RoutingRule, error) {
	idx := m.routingRuleIndex(name)
	if idx < 0 {
		return RoutingRule{}, ErrRoutingRuleNotFound
	}
	if err := routing.NewRoutingRuleValidation().ValidateUpdate(update); err != nil {
		return RoutingRule{}, err
	}
	update.Name = name
	next := m.RoutingRules()
	next[idx] = update
	if err := m.replaceRoutingRules(next); err != nil {
		return RoutingRule{}, err
	}
	return update, nil
}

func (m Mutation) DeleteRoutingRule(name string) error {
	idx := m.routingRuleIndex(name)
	if idx < 0 {
		return ErrRoutingRuleNotFound
	}
	next := m.RoutingRules()
	next = append(next[:idx], next[idx+1:]...)
	return m.replaceRoutingRules(next)
}

func (m Mutation) routingRuleIndex(name string) int {
	if m.target.Rules == nil {
		return -1
	}
	return routing.NewRoutingRuleIndex(*m.target.Rules).Index(name)
}

func (m Mutation) replaceRoutingRules(next []RoutingRule) error {
	if m.target.Rules != nil {
		*m.target.Rules = next
	}
	return m.save()
}

func (m Mutation) Warp() WarpConfig {
	if m.target.Warp == nil {
		return WarpConfig{}
	}
	return veilwarp.Redact(*m.target.Warp)
}

func (m Mutation) UpdateWarp(update WarpConfig) (WarpConfig, error) {
	current := WarpConfig{}
	if m.target.Warp != nil {
		current = *m.target.Warp
	}
	update = veilwarp.PreserveRedacted(update, current)
	veilwarp.SetDefaults(&update)
	if m.target.Warp != nil {
		*m.target.Warp = update
	}

	if m.target.Rules != nil {
		if update.Enabled {
			hasWarp := false
			for _, r := range *m.target.Rules {
				if r.Outbound == "warp" {
					hasWarp = true
					break
				}
			}
			if !hasWarp {
				warpRule := RoutingRule{
					Name:     "warp-routing",
					Match:    "geosite:openai",
					Outbound: "warp",
					Enabled:  true,
				}
				*m.target.Rules = append([]RoutingRule{warpRule}, (*m.target.Rules)...)
			}
		} else {
			nextRules := []RoutingRule{}
			for _, r := range *m.target.Rules {
				if r.Outbound != "warp" {
					nextRules = append(nextRules, r)
				}
			}
			*m.target.Rules = nextRules
		}
	}

	if err := m.save(); err != nil {
		return WarpConfig{}, err
	}
	return veilwarp.Redact(update), nil
}
