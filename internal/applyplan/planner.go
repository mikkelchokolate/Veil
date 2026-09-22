package applyplan

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/hostenv"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/routing"
)

type Material struct {
	Configs  []string
	Actions  []string
	Runtimes []string
	Errors   []string
}

type ProtocolCapability struct {
	Protocol               string
	Config                 string
	ConfigForInbound       func(model.Inbound) string
	Action                 string
	ActionForInbound       func(model.Inbound) string
	ValidateSettings       func(model.Settings, model.Inbound) error
	ValidateInboundRender  bool
	RequiresRenderSettings bool
}

type Input struct {
	Settings                model.Settings
	Inbounds                []model.Inbound
	Rules                   []model.RoutingRule
	RoutingSource           model.RoutingSource
	Warp                    model.WarpConfig
	RenderSettingsAvailable bool
	PanelAccess             Material
	Capabilities            []ProtocolCapability
	ValidateCardinality     func(model.Settings, []model.Inbound) error
	RuntimeUnits            []string
	WarpAction              string
	ValidateInboundRender   func(model.Inbound) error
	ValidateWarpRender      func() error
	GeneratedRoot           string
	LiveRoot                string
}

func Build(input Input) model.ApplyPlanResponse {
	// Displayed config paths are anchored at the live generated root so the
	// preview matches the promote destinations — including custom live roots
	// (issue #636). Empty falls back to the configured etc dir's generated
	// tree so zero-arg previews stay consistent with the install layout.
	liveRoot := filepath.ToSlash(input.LiveRoot)
	if liveRoot == "" {
		liveRoot = filepath.ToSlash(filepath.Join(hostenv.EtcDir(), "generated"))
	}
	plan := model.ApplyPlanResponse{
		Valid:      true,
		Configs:    []string{},
		Actions:    []string{"validate management state"},
		Issues:     []model.ValidationIssue{},
		Operations: []model.ApplyOperation{},
	}
	appendMaterial(&plan, input.PanelAccess)
	seen := map[string]bool{}
	capabilities := protocolCapabilities(input.Capabilities)
	for _, inbound := range input.Inbounds {
		if !inbound.Enabled {
			continue
		}
		if inbound.Name == "" || inbound.Protocol == "" || inbound.Transport == "" {
			plan.Errors = append(plan.Errors, "enabled inbounds require name, protocol, and transport")
		}
		if inbound.Port <= 0 {
			plan.Errors = append(plan.Errors, "enabled inbounds require a positive port")
		}
		key := inbound.Transport + ":" + fmt.Sprint(inbound.Port)
		if seen[key] {
			plan.Errors = append(plan.Errors, "duplicate enabled inbound transport/port")
		}
		seen[key] = true
		capability, ok := capabilities[inbound.Protocol]
		if !ok {
			if inbound.Protocol != "" {
				plan.Errors = append(plan.Errors, "unsupported inbound protocol: "+inbound.Protocol)
			}
			continue
		}
		if capability.ValidateSettings != nil {
			if err := capability.ValidateSettings(input.Settings, inbound); err != nil {
				plan.Errors = append(plan.Errors, err.Error())
			}
		}
		config := capability.Config
		if capability.ConfigForInbound != nil {
			if perInboundConfig := capability.ConfigForInbound(inbound); perInboundConfig != "" {
				config = perInboundConfig
			}
		}
		plan.Configs = appendUnique(plan.Configs, config)
		action := capability.Action
		if capability.ActionForInbound != nil {
			if perInboundAction := capability.ActionForInbound(inbound); perInboundAction != "" {
				action = perInboundAction
			}
		}
		plan.Actions = appendUnique(plan.Actions, action)
		if capability.ShouldValidateRender(input.RenderSettingsAvailable) && input.ValidateInboundRender != nil {
			if err := input.ValidateInboundRender(inbound); err != nil {
				plan.Errors = append(plan.Errors, err.Error())
			}
		}
	}
	if input.ValidateCardinality != nil {
		if err := input.ValidateCardinality(input.Settings, input.Inbounds); err != nil {
			plan.Errors = append(plan.Errors, err.Error())
		}
	}
	for _, unit := range input.RuntimeUnits {
		plan.Runtimes = appendUnique(plan.Runtimes, unit)
	}
	if input.Warp.Enabled {
		plan.Configs = appendUnique(plan.Configs, liveRoot+"/sing-box/warp.json")
		if input.WarpAction != "" {
			plan.Actions = appendUnique(plan.Actions, input.WarpAction)
		}
		if input.ValidateWarpRender != nil {
			if err := input.ValidateWarpRender(); err != nil {
				plan.Errors = append(plan.Errors, err.Error())
			}
		}
	}
	for _, rule := range input.Rules {
		if !rule.Enabled {
			continue
		}
		if rule.Name == "" || rule.Match == "" || rule.Outbound == "" {
			plan.Errors = append(plan.Errors, "enabled routing rules require name, match, and outbound")
			continue
		}
		switch rule.Outbound {
		case "direct", "proxy":
		case "warp":
			if !input.Warp.Enabled {
				plan.Errors = append(plan.Errors, "routing rule "+rule.Name+" requires WARP to be enabled")
			}
		default:
			plan.Errors = append(plan.Errors, "unsupported routing outbound: "+rule.Outbound)
		}
	}
	for _, file := range input.RoutingSource.Files {
		if file.Name == "" || file.URL == "" {
			plan.Errors = append(plan.Errors, "routing source files require name and URL")
			continue
		}
		plan.Configs = appendUnique(plan.Configs, liveRoot+"/rules/"+file.Name)
	}
	appendHysteria2ACLRuleIssues(&plan, input)
	if len(plan.Configs) > 0 {
		plan.Actions = append([]string{"validate management state", "stage generated configs"}, plan.Actions[1:]...)
	}
	if len(plan.Errors) > 0 {
		plan.Valid = false
	}
	plan.Operations = buildOperations(plan.Configs, plan.Actions, plan.Runtimes, input.GeneratedRoot, liveRoot)
	return plan
}

func buildOperations(configs, actions, runtimes []string, generatedRoot, liveRoot string) []model.ApplyOperation {
	var operations []model.ApplyOperation
	sortedConfigs := append([]string(nil), configs...)
	sort.Strings(sortedConfigs)
	for _, config := range sortedConfigs {
		relative := generatedRelativePath(config, liveRoot)
		source := config
		if generatedRoot != "" {
			source = filepath.ToSlash(filepath.Join(generatedRoot, filepath.FromSlash(relative)))
		}
		destination := config
		if liveRoot != "" {
			destination = filepath.ToSlash(filepath.Join(liveRoot, filepath.FromSlash(relative)))
		}
		operations = append(operations, model.ApplyOperation{
			Type:              "promote_file",
			Source:            filepath.ToSlash(source),
			Destination:       filepath.ToSlash(destination),
			InterruptionRisk:  "reload",
			RollbackAvailable: true,
			ValidationSource:  "render-and-live-host",
		})
	}

	serviceVerbs := serviceActionVerbs(actions)
	units := append([]string(nil), runtimes...)
	for unit := range serviceVerbs {
		units = appendUnique(units, unit)
	}
	sort.Strings(units)
	for _, unit := range units {
		verb := serviceVerbForUnit(unit, serviceVerbs)
		if verb != "reload" && verb != "restart" {
			continue
		}
		risk := "reload"
		if verb == "restart" {
			risk = "connection-drop"
		}
		operations = append(operations, model.ApplyOperation{
			Type:              verb + "_service",
			Unit:              unit,
			InterruptionRisk:  risk,
			RollbackAvailable: true,
			ValidationSource:  "managed-unit-catalog",
		})
	}
	return operations
}

func generatedRelativePath(config, liveRoot string) string {
	slashPath := filepath.ToSlash(config)
	if liveRoot != "" {
		root := strings.TrimSuffix(filepath.ToSlash(liveRoot), "/")
		if slashPath == root {
			return ""
		}
		if strings.HasPrefix(slashPath, root+"/") {
			return strings.TrimPrefix(slashPath, root+"/")
		}
	}
	if index := strings.Index(slashPath, "/generated/"); index >= 0 {
		return strings.TrimPrefix(slashPath[index+len("/generated/"):], "/")
	}
	return filepath.ToSlash(filepath.Base(config))
}

func serviceActionVerbs(actions []string) map[string]string {
	verbs := map[string]string{}
	for _, action := range actions {
		fields := strings.Fields(action)
		if len(fields) != 2 {
			continue
		}
		if fields[0] != "reload" && fields[0] != "restart" {
			continue
		}
		verbs[fields[1]] = fields[0]
	}
	return verbs
}

func serviceVerbForUnit(unit string, verbs map[string]string) string {
	if verb := verbs[unit]; verb != "" {
		return verb
	}
	for actionUnit, verb := range verbs {
		if !strings.Contains(actionUnit, "@.service") {
			continue
		}
		prefix := strings.TrimSuffix(actionUnit, "@.service") + "@"
		if strings.HasPrefix(unit, prefix) && strings.HasSuffix(unit, ".service") {
			return verb
		}
	}
	return ""
}

// appendHysteria2ACLRuleIssues surfaces routing-rule atoms the Hysteria2 ACL
// renderer must drop. apernet/hysteria implements only geoip:/geosite:/
// suffix:/bare-CIDR-or-IP/wildcard/exact-domain addresses — keyword: and
// regexp: atoms, and values carrying grammar-breaking characters like `#`,
// have no native form and would previously be emitted as fake prefixes that
// silently misrouted or failed the unit's config load (#679). The renderer
// now omits them; this warning keeps the loss loud in the apply plan while
// sing-box (WARP) keeps full matcher semantics.
func appendHysteria2ACLRuleIssues(plan *model.ApplyPlanResponse, input Input) {
	if plan == nil || !input.Warp.Enabled {
		return
	}
	hasHysteria2 := false
	for _, inbound := range input.Inbounds {
		if inbound.Enabled && inbound.Protocol == "hysteria2" {
			hasHysteria2 = true
			break
		}
	}
	if !hasHysteria2 {
		return
	}
	for _, rule := range input.Rules {
		if !rule.Enabled || rule.Match == "" {
			continue
		}
		matchers, err := routing.ParseMatch(rule.Match)
		if err != nil {
			continue
		}
		for _, matcher := range matchers {
			if _, ok := routing.Hysteria2ACLAddress(matcher); ok {
				continue
			}
			plan.Issues = append(plan.Issues, model.ValidationIssue{
				Code:        "hysteria2_acl_unsupported_match",
				Severity:    "warning",
				Field:       "match",
				Message:     fmt.Sprintf("routing rule %q match %q uses a matcher Hysteria2 ACL cannot express; that atom is skipped for Hysteria2 inbounds", rule.Name, rule.Match),
				Remediation: "Use domain, domain-suffix, geoip, geosite, or CIDR matches for rules that must apply to Hysteria2.",
				Source:      "hysteria2",
			})
			break
		}
	}
}

func appendMaterial(plan *model.ApplyPlanResponse, material Material) {
	for _, config := range material.Configs {
		plan.Configs = appendUnique(plan.Configs, config)
	}
	for _, action := range material.Actions {
		plan.Actions = appendUnique(plan.Actions, action)
	}
	for _, runtime := range material.Runtimes {
		plan.Runtimes = appendUnique(plan.Runtimes, runtime)
	}
	plan.Errors = append(plan.Errors, material.Errors...)
}

func protocolCapabilities(capabilities []ProtocolCapability) map[string]ProtocolCapability {
	byProtocol := map[string]ProtocolCapability{}
	for _, capability := range capabilities {
		byProtocol[capability.Protocol] = capability
	}
	return byProtocol
}

func (c ProtocolCapability) ShouldValidateRender(renderSettingsAvailable bool) bool {
	return c.ValidateInboundRender && (!c.RequiresRenderSettings || renderSettingsAvailable)
}

func appendUnique(values []string, next string) []string {
	if next == "" {
		return values
	}
	for _, value := range values {
		if value == next {
			return values
		}
	}
	return append(values, next)
}
