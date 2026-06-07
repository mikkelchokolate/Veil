package livevalidation

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/protocols"
)

type runtimeRequirement struct {
	binary string
	unit   func(model.Inbound) string
}

var runtimeRequirements = map[string]runtimeRequirement{
	"naiveproxy": {
		binary: "caddy",
		unit: func(inbound model.Inbound) string {
			return "veil-caddy@" + inbound.Name + ".service"
		},
	},
	"hysteria2": {
		binary: "hysteria",
		unit: func(inbound model.Inbound) string {
			return "veil-hysteria2@" + inbound.Name + ".service"
		},
	},
	"olcrtc": {
		binary: "olcrtc",
		unit: func(inbound model.Inbound) string {
			return "veil-olcrtc@" + inbound.Name + ".service"
		},
	},
	"mieru": {
		// mieru's server binary is "mita" (the "mieru" binary is the client).
		binary: "mita",
		unit: func(model.Inbound) string {
			return "veil-mieru.service"
		},
	},
}

func (v Validator) Validate(ctx context.Context, request Request) Response {
	now := time.Now
	if v.Now != nil {
		now = v.Now
	}
	response := Response{Valid: true, Issues: []model.ValidationIssue{}, CheckedAt: now().UTC()}
	seen := map[string]string{}
	panelPort := parseListenPort(request.Settings.PanelListen)
	needsDNS := false

	for _, inbound := range request.Inbounds {
		if !inbound.Enabled {
			continue
		}
		issues := v.validateInbound(ctx, request, inbound, panelPort, seen)
		response.Issues = append(response.Issues, issues...)
		if protocolNeedsDomain(inbound.Protocol) {
			needsDNS = true
		}
	}

	if needsDNS && strings.TrimSpace(request.Settings.Domain) != "" && v.DNS != nil {
		if addresses, err := v.DNS.LookupHost(ctx, request.Settings.Domain); err != nil || len(addresses) == 0 {
			response.Issues = append(response.Issues, issue(
				"dns_unresolved",
				SeverityWarning,
				"settings.domain",
				"",
				"Configured domain does not resolve",
				"Create or correct the DNS record before applying this configuration.",
				"live-host",
			))
		}
	}

	sortIssues(response.Issues)
	for _, item := range response.Issues {
		if item.Severity == SeverityError {
			response.Valid = false
			break
		}
	}
	return response
}

func (v Validator) validateInbound(
	ctx context.Context,
	request Request,
	inbound model.Inbound,
	panelPort int,
	seen map[string]string,
) []model.ValidationIssue {
	issues := []model.ValidationIssue{}
	id := inbound.Name
	if strings.TrimSpace(inbound.Name) == "" {
		issues = append(issues, issue(
			"name_required", SeverityError, "name", id,
			"Enabled inbound requires a name",
			"Choose a unique inbound name.", "candidate",
		))
	}

	capability, supported := protocols.NewCapabilityCatalog().ForProtocol(inbound.Protocol)
	if !supported {
		issues = append(issues, issue(
			"unsupported_protocol", SeverityError, "protocol", id,
			"Unsupported inbound protocol",
			"Choose one of the protocols offered by Veil.", "candidate",
		))
	}
	if strings.TrimSpace(inbound.Transport) == "" {
		issues = append(issues, issue(
			"transport_required", SeverityError, "transport", id,
			"Enabled inbound requires a transport",
			"Choose a transport supported by this protocol.", "candidate",
		))
	} else if supported && !contains(capability.Transports, inbound.Transport) {
		issues = append(issues, issue(
			"unsupported_transport", SeverityError, "transport", id,
			"Transport is not supported by this protocol",
			"Choose a transport listed for the selected protocol.", "candidate",
		))
	}
	if inbound.Port < 1 || inbound.Port > 65535 {
		issues = append(issues, issue(
			"port_invalid", SeverityError, "port", id,
			"Inbound port must be between 1 and 65535",
			"Choose a valid TCP or UDP port.", "candidate",
		))
	}

	if inbound.Transport != "" && inbound.Port >= 1 && inbound.Port <= 65535 {
		key := bindingKey(inbound.Transport, inbound.Port)
		if previous, exists := seen[key]; exists {
			issues = append(issues, issue(
				"duplicate_binding", SeverityError, "port", id,
				fmt.Sprintf("%s port %d is already assigned to %s", strings.ToUpper(inbound.Transport), inbound.Port, previous),
				"Choose another port or disable the other inbound.", "candidate",
			))
		} else {
			seen[key] = displayInboundID(inbound)
		}

		if inbound.Transport == "tcp" && panelPort > 0 && inbound.Port == panelPort {
			issues = append(issues, issue(
				"reserved_panel_port", SeverityError, "port", id,
				fmt.Sprintf("TCP port %d is reserved by the Panel", inbound.Port),
				"Choose another inbound port or move the Panel listener.", "candidate",
			))
		}

		if v.Ports != nil && !ownedBinding(inbound, request.CurrentInbounds) {
			available, err := v.Ports.Available(ctx, inbound.Transport, inbound.Port)
			if err != nil {
				issues = append(issues, issue(
					"port_probe_failed", SeverityError, "port", id,
					fmt.Sprintf("Could not check %s port %d", strings.ToUpper(inbound.Transport), inbound.Port),
					"Check host permissions and retry validation.", "live-host",
				))
			} else if !available {
				issues = append(issues, issue(
					"port_in_use", SeverityError, "port", id,
					fmt.Sprintf("%s port %d is already in use", strings.ToUpper(inbound.Transport), inbound.Port),
					"Stop the conflicting service or choose another port.", "live-host",
				))
			}
		}
	}

	issues = append(issues, requiredFieldIssues(request.Settings, inbound)...)
	if supported {
		issues = append(issues, v.runtimeIssues(ctx, inbound)...)
	}
	return issues
}

func (v Validator) runtimeIssues(ctx context.Context, inbound model.Inbound) []model.ValidationIssue {
	requirement, ok := runtimeRequirements[inbound.Protocol]
	if !ok {
		return nil
	}
	issues := []model.ValidationIssue{}
	if v.Binaries != nil {
		if _, err := v.Binaries.LookPath(requirement.binary); err != nil {
			issues = append(issues, issue(
				"runtime_binary_missing", SeverityWarning, "protocol", inbound.Name,
				fmt.Sprintf("Required runtime binary %s is not installed", requirement.binary),
				"Install the protocol runtime before applying.", "live-host",
			))
		}
	}
	if v.Units != nil {
		unit := requirement.unit(inbound)
		exists, err := v.Units.Exists(ctx, unit)
		if err != nil || !exists {
			issues = append(issues, issue(
				"runtime_unit_missing", SeverityWarning, "protocol", inbound.Name,
				fmt.Sprintf("Required systemd unit %s is unavailable", unit),
				"Install or repair Veil managed service units.", "live-host",
			))
		}
	}
	return issues
}

func requiredFieldIssues(settings model.Settings, inbound model.Inbound) []model.ValidationIssue {
	issues := []model.ValidationIssue{}
	if protocolNeedsDomain(inbound.Protocol) && strings.TrimSpace(settings.Domain) == "" {
		issues = append(issues, issue(
			"domain_required", SeverityError, "settings.domain", inbound.Name,
			"This protocol requires a public domain",
			"Set the domain that resolves to this host.", "candidate",
		))
	}
	if inbound.Protocol == "naiveproxy" && strings.TrimSpace(settings.Email) == "" {
		issues = append(issues, issue(
			"email_required", SeverityError, "settings.email", inbound.Name,
			"NaiveProxy automatic TLS requires an email address",
			"Set the ACME contact email.", "candidate",
		))
	}
	if !hasCredential(settings, inbound) {
		issues = append(issues, issue(
			"credential_required", SeverityError, "password", inbound.Name,
			"This inbound has no usable client credential",
			"Set an inbound credential or enable a client profile with credentials.", "candidate",
		))
	}
	return issues
}

func protocolNeedsDomain(protocol string) bool {
	switch protocol {
	case "naiveproxy", "hysteria2":
		return true
	default:
		return false
	}
}

func hasCredential(settings model.Settings, inbound model.Inbound) bool {
	for _, profile := range inbound.Profiles {
		if !profile.Enabled || strings.TrimSpace(profile.Password) == "" {
			continue
		}
		if inbound.Protocol != "naiveproxy" || strings.TrimSpace(profile.Username) != "" {
			return true
		}
	}
	switch inbound.Protocol {
	case "naiveproxy":
		username := firstNonEmpty(inbound.NaiveUsername, settings.NaiveUsername)
		password := firstNonEmpty(inbound.Password, inbound.NaivePassword, settings.NaivePassword)
		return username != "" && password != ""
	case "hysteria2":
		return firstNonEmpty(inbound.Password, inbound.Hysteria2Password, settings.Hysteria2Password) != ""
	case "mieru", "olcrtc":
		return strings.TrimSpace(inbound.Password) != ""
	default:
		return true
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func ownedBinding(candidate model.Inbound, current []model.Inbound) bool {
	for _, inbound := range current {
		if inbound.Enabled &&
			inbound.Name == candidate.Name &&
			inbound.Protocol == candidate.Protocol &&
			inbound.Transport == candidate.Transport &&
			inbound.Port == candidate.Port {
			return true
		}
	}
	return false
}

func bindingKey(transport string, port int) string {
	return strings.ToLower(strings.TrimSpace(transport)) + ":" + strconv.Itoa(port)
}

func parseListenPort(listen string) int {
	_, portText, err := net.SplitHostPort(strings.TrimSpace(listen))
	if err != nil {
		return 0
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return 0
	}
	return port
}

func issue(code, severity, field, inboundID, message, remediation, source string) model.ValidationIssue {
	return model.ValidationIssue{
		Code:        code,
		Severity:    severity,
		Field:       field,
		InboundID:   inboundID,
		Message:     message,
		Remediation: remediation,
		Source:      source,
	}
}

func displayInboundID(inbound model.Inbound) string {
	if inbound.Name != "" {
		return inbound.Name
	}
	return "another inbound"
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func sortIssues(issues []model.ValidationIssue) {
	sort.SliceStable(issues, func(i, j int) bool {
		left := issueSortKey(issues[i])
		right := issueSortKey(issues[j])
		for index := range left {
			if left[index] == right[index] {
				continue
			}
			return left[index] < right[index]
		}
		return false
	})
}

func sortIssueKeys(keys []string) {
	sort.SliceStable(keys, func(i, j int) bool {
		return normalizeIssueSortKey(keys[i]) < normalizeIssueSortKey(keys[j])
	})
}

func issueSortKey(value model.ValidationIssue) [4]string {
	return [4]string{severityRank(value.Severity), value.InboundID, value.Field, value.Code}
}

func normalizeIssueSortKey(value string) string {
	parts := strings.SplitN(value, ":", 4)
	if len(parts) != 4 {
		return value
	}
	return severityRank(parts[0]) + ":" + parts[1] + ":" + parts[2] + ":" + parts[3]
}

func severityRank(severity string) string {
	switch severity {
	case SeverityError:
		return "0"
	case SeverityWarning:
		return "1"
	case SeverityInfo:
		return "2"
	default:
		return "3"
	}
}
