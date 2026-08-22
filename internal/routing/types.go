package routing

import (
	"errors"

	"github.com/mikkelchokolate/Veil/internal/model"
)

var (
	ErrRuleInvalid         = errors.New("routing rule invalid")
	ErrRuleNotFound        = errors.New("routing rule not found")
	ErrRuleDuplicateName   = errors.New("routing rule name already exists")
	ErrRoutingMatchInvalid = errors.New("routing match is invalid")
)

var (
	ErrRoutingRuleInvalid       = ErrRuleInvalid
	ErrRoutingRuleNotFound      = ErrRuleNotFound
	ErrRoutingRuleDuplicateName = ErrRuleDuplicateName
)

type Rule = model.RoutingRule
type RoutingRule = model.RoutingRule
type Source = model.RoutingSource
type RoutingSource = model.RoutingSource
type SourceFile = model.RoutingSourceFile
type RoutingSourceFile = model.RoutingSourceFile
type Preset = model.RoutingPreset
type RoutingPreset = model.RoutingPreset
type PresetResponse = model.RoutingPresetResponse
type RoutingPresetResponse = model.RoutingPresetResponse

type RuleValidation = RoutingRuleValidation
type RuleIndex = RoutingRuleIndex

func NewRuleValidation() RuleValidation   { return NewRoutingRuleValidation() }
func NewRuleIndex(rules []Rule) RuleIndex { return NewRoutingRuleIndex(rules) }

func PresetProfiles() []RoutingPreset                { return routingPresetProfiles() }
func PresetByName(name string) (RoutingPreset, bool) { return routingPresetByName(name) }
func RouteDatSource() RoutingSource                  { return routeDatSource() }
