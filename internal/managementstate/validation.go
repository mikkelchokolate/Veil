package managementstate

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/inbounds"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/protocols"
	"github.com/mikkelchokolate/Veil/internal/runtimeports"
)

type Validation struct{}

type ValidationResult struct {
	Valid  bool
	Errors []string
}

func NewValidation() Validation { return Validation{} }

func (v Validation) ValidateBytes(body []byte) (ValidationResult, error) {
	snapshot, err := NewManagementStateCodec().Decode(body)
	if err != nil {
		if syntaxErr := DecodeError(err); syntaxErr != nil {
			return ValidationResult{}, syntaxErr
		}
		return ValidationResult{Valid: false, Errors: []string{"state: invalid JSON: " + err.Error()}}, nil
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(body, &fields); err != nil {
		return ValidationResult{}, err
	}
	errs := v.ValidateSnapshot(snapshot, fields)
	return ValidationResult{Valid: len(errs) == 0, Errors: errs}, nil
}

func (Validation) ValidateSnapshot(snapshot model.ManagementSnapshot, fields map[string]json.RawMessage) []string {
	var errs []string
	seenPorts := map[string]string{}
	protocolCatalog := protocols.NewCatalog()

	hysteriaStatsActive := false
	caddyActive := false
	for _, inbound := range snapshot.Inbounds {
		if !inbound.Enabled {
			continue
		}
		if inbound.Protocol == "hysteria2" {
			hysteriaStatsActive = true
		}
		if inbound.Protocol == "naiveproxy" {
			caddyActive = true
		}
	}
	panelCaddyPort, panelCaddyActive := effectivePanelCaddyPort(snapshot.Settings)
	if panelCaddyActive {
		caddyActive = true
	}

	if panelCaddyActive {
		if hysteriaStatsActive && panelCaddyPort == runtimeports.Hysteria2TrafficStatsPort {
			errs = append(errs, fmt.Sprintf("settings.panelPublicPort: TCP port %d is reserved for Hysteria2 traffic statistics", panelCaddyPort))
		}
		if panelCaddyPort == runtimeports.CaddyAdminPort {
			errs = append(errs, fmt.Sprintf("settings.panelPublicPort: TCP port %d conflicts with the Caddy admin listener", panelCaddyPort))
		}
	}

	if _, ok := fields["settings"]; ok {
		if snapshot.Settings.PanelListen == "" {
			errs = append(errs, "settings.panelListen is required")
		} else if _, portStr, err := net.SplitHostPort(snapshot.Settings.PanelListen); err == nil {
			if port, err := strconv.Atoi(portStr); err == nil {
				if hysteriaStatsActive && port == runtimeports.Hysteria2TrafficStatsPort {
					errs = append(errs, fmt.Sprintf("settings.panelListen: TCP port %d is reserved for Hysteria2 traffic statistics", port))
				}
				if caddyActive && port == runtimeports.CaddyAdminPort {
					errs = append(errs, fmt.Sprintf("settings.panelListen: TCP port %d conflicts with the Caddy admin listener", port))
				}
				if panelCaddyActive && port == panelCaddyPort {
					errs = append(errs, fmt.Sprintf("settings.panelListen: TCP port %d conflicts with the Caddy public panel listener", port))
				}
				seenPorts["tcp:"+itoa(port)] = "panel"
			}
		}
		if snapshot.Settings.Mode == "" {
			errs = append(errs, "settings.mode is required")
		}
	} else {
		errs = append(errs, "settings is missing")
	}

	if _, ok := fields["warp"]; ok && snapshot.Warp.Enabled {
		port := snapshot.Warp.SocksPort
		if port == 0 {
			port = 40000
		}
		if hysteriaStatsActive && port == runtimeports.Hysteria2TrafficStatsPort {
			errs = append(errs, fmt.Sprintf("warp.socksPort: TCP port %d is reserved for Hysteria2 traffic statistics", port))
		}
		if caddyActive && port == runtimeports.CaddyAdminPort {
			errs = append(errs, fmt.Sprintf("warp.socksPort: TCP port %d conflicts with the Caddy admin listener", port))
		}
		if panelCaddyActive && port == panelCaddyPort {
			errs = append(errs, fmt.Sprintf("warp.socksPort: TCP port %d conflicts with the Caddy public panel listener", port))
		}
		seenPorts["tcp:"+itoa(port)] = "warp"
		seenPorts["udp:"+itoa(port)] = "warp"
	}

	if _, ok := fields["inbounds"]; ok {
		seenInboundNames := map[string]int{}
		for i, inbound := range snapshot.Inbounds {
			if inbound.Name == "" {
				errs = append(errs, "inbounds["+itoa(i)+"].name is required")
			} else if !inbounds.IsSafeName(inbound.Name) {
				errs = append(errs, "inbounds["+itoa(i)+"].name must contain only letters, digits, underscore, or hyphen")
			} else if prev, dup := seenInboundNames[inbound.Name]; dup {
				errs = append(errs, fmt.Sprintf("inbounds[%d]: duplicate name %q also used by inbounds[%d]", i, inbound.Name, prev))
			} else {
				seenInboundNames[inbound.Name] = i
			}
			if inbound.Protocol == "" {
				errs = append(errs, "inbounds["+itoa(i)+"].protocol is required")
			}
			if inbound.Transport == "" {
				errs = append(errs, "inbounds["+itoa(i)+"].transport is required")
			}
			if inbound.Protocol != "" && inbound.Transport != "" && !protocolCatalog.SupportsTransport(inbound.Protocol, inbound.Transport) {
				errs = append(errs, "inbounds["+itoa(i)+"].protocol/transport is unsupported: "+inbound.Protocol+"/"+inbound.Transport)
			}
			if inbound.Port <= 0 || inbound.Port > 65535 {
				errs = append(errs, "inbounds["+itoa(i)+"].port must be 1-65535, got: "+itoa(inbound.Port))
			}

			// NaiveProxy's actual Caddy bind can be overridden independently via
			// protocolFields.publicPort. Collision detection must use that effective
			// port; checking only the legacy inbound.Port lets a desired state pass
			// validation and fail when Caddy attempts to bind the real listener.
			bindTransport := inbound.Transport
			bindPort := inbound.Port
			if inbound.Protocol == "naiveproxy" {
				bindTransport = "tcp"
				bindPort = model.ResolveNaivePublicPort(snapshot.Settings, inbound)
			}

			if bindTransport == "tcp" {
				if hysteriaStatsActive && bindPort == runtimeports.Hysteria2TrafficStatsPort {
					errs = append(errs, fmt.Sprintf("inbounds[%d]: effective TCP port %d is reserved for Hysteria2 traffic statistics", i, bindPort))
				}
				if caddyActive && bindPort == runtimeports.CaddyAdminPort {
					errs = append(errs, fmt.Sprintf("inbounds[%d]: effective TCP port %d conflicts with the Caddy admin listener", i, bindPort))
				}
				// Panel Caddy and NaiveProxy intentionally share one Caddy server/bind;
				// any other TCP runtime on that wildcard port is a real collision.
				if panelCaddyActive && inbound.Protocol != "naiveproxy" && bindPort == panelCaddyPort {
					errs = append(errs, fmt.Sprintf("inbounds[%d]: effective TCP port %d conflicts with the Caddy public panel listener", i, bindPort))
				}
			}

			key := bindTransport + ":" + itoa(bindPort)
			if owner, exists := seenPorts[key]; exists {
				if owner == "panel" || owner == "warp" {
					errs = append(errs, fmt.Sprintf("inbounds[%d]: port %d conflicts with %s", i, bindPort, owner))
				} else {
					errs = append(errs, "inbounds["+itoa(i)+"]: duplicate transport/port "+key)
				}
			}
			seenPorts[key] = "inbounds[" + itoa(i) + "]"
		}
	}
	if _, ok := fields["routingRules"]; ok {
		seenRuleNames := map[string]int{}
		for i, rule := range snapshot.Rules {
			if rule.Name == "" {
				errs = append(errs, "routingRules["+itoa(i)+"].name is required")
			} else if prev, dup := seenRuleNames[rule.Name]; dup {
				errs = append(errs, fmt.Sprintf("routingRules[%d]: duplicate name %q also used by routingRules[%d]", i, rule.Name, prev))
			} else {
				seenRuleNames[rule.Name] = i
			}
			if rule.Match == "" {
				errs = append(errs, "routingRules["+itoa(i)+"].match is required")
			}
			if rule.Outbound == "" {
				errs = append(errs, "routingRules["+itoa(i)+"].outbound is required")
			}
		}
	}
	return errs
}

func effectivePanelCaddyPort(settings model.Settings) (int, bool) {
	if settings.PanelAccess != "caddy" {
		return 0, false
	}
	domain := strings.TrimSpace(settings.PanelDomain)
	if domain == "" {
		domain = strings.TrimSpace(settings.Domain)
	}
	if domain == "" {
		return 0, false
	}
	port := settings.PanelPublicPort
	if port == 0 {
		port = 443
	}
	return port, true
}

func itoa(value int) string {
	return strconv.Itoa(value)
}

func AppendUnique(values []string, next string) []string {
	for _, value := range values {
		if value == next {
			return values
		}
	}
	return append(values, next)
}
