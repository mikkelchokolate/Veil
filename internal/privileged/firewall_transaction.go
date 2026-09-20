package privileged

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type ufwState struct {
	Enabled bool              `json:"enabled"`
	Rules   map[string]string `json:"rules"`
	Entries []ufwRuleState    `json:"entries"`
}

type ufwRuleState struct {
	Family      string   `json:"family"`
	Action      string   `json:"action"`
	Direction   string   `json:"direction"`
	Interface   string   `json:"interface,omitempty"`
	Source      string   `json:"source"`
	Destination string   `json:"destination"`
	Protocol    string   `json:"protocol,omitempty"`
	Order       int      `json:"order"`
	Enabled     bool     `json:"enabled"`
	Comment     string   `json:"comment,omitempty"`
	Args        []string `json:"args"`
}

type ufwDesiredRule struct {
	id      string
	target  string
	comment string
	args    []string
}

func reconcileUFW(ctx context.Context, runner CommandRunner, request ResolvedFirewall) (FirewallResult, error) {
	if runner == nil {
		return FirewallResult{}, errors.New("firewall runner is unavailable")
	}
	desired, err := parseDesiredUFWRules(request)
	if err != nil {
		return FirewallResult{}, err
	}
	initialOutput, err := runUFW(ctx, runner, 10*time.Second, "status")
	if err != nil {
		return FirewallResult{}, fmt.Errorf("preflight ufw status: %w", err)
	}
	initial, err := parseUFWStatus(initialOutput)
	if err != nil {
		return FirewallResult{}, fmt.Errorf("parse preflight ufw status: %w", err)
	}
	if len(desired) > 0 && !containsManagementAccessRule(desired) && !hasExistingManagementAccess(initial) {
		return FirewallResult{}, errors.New("refusing to enable UFW without a staged SSH or Panel management access rule")
	}

	rollback := func(cause error) error {
		if restoreErr := restoreUFWState(ctx, runner, initial, desired); restoreErr != nil {
			return fmt.Errorf("%v; restore previous UFW state: %w", cause, restoreErr)
		}
		return cause
	}

	// Validate every requested command without touching live rules.
	for _, rule := range desired {
		args := append([]string{"--dry-run"}, rule.args...)
		if _, err := runUFW(ctx, runner, 10*time.Second, args...); err != nil {
			return FirewallResult{}, rollback(fmt.Errorf("dry-run firewall rule %s: %w", rule.id, err))
		}
	}
	// Add/replace all desired access before deleting anything or enabling UFW.
	for _, rule := range desired {
		if _, err := runUFW(ctx, runner, 10*time.Second, rule.args...); err != nil {
			return FirewallResult{}, rollback(fmt.Errorf("stage firewall rule %s: %w", rule.id, err))
		}
	}
	desiredTargets := make(map[string]struct{}, len(desired))
	for _, rule := range desired {
		desiredTargets[rule.target] = struct{}{}
	}
	staleTargets := make([]string, 0)
	for target, comment := range initial.Rules {
		if !isVeilManagedFirewallComment(comment) || isProtectedVeilFirewallComment(comment) {
			continue
		}
		if _, keep := desiredTargets[target]; !keep {
			staleTargets = append(staleTargets, target)
		}
	}
	sort.Strings(staleTargets)
	for _, target := range staleTargets {
		if _, err := runUFW(ctx, runner, 10*time.Second, "delete", "allow", target); err != nil {
			return FirewallResult{}, rollback(fmt.Errorf("delete stale Veil firewall rule %s: %w", target, err))
		}
	}
	if !initial.Enabled {
		// An empty desired set only prunes stale Veil-managed rules; it must
		// never enable UFW, because no management access rule is staged.
		if len(desired) > 0 {
			if _, err := runUFW(ctx, runner, 20*time.Second, "--force", "enable"); err != nil {
				return FirewallResult{}, rollback(fmt.Errorf("enable ufw: %w", err))
			}
		}
	} else {
		if _, err := runUFW(ctx, runner, 20*time.Second, "reload"); err != nil {
			return FirewallResult{}, rollback(fmt.Errorf("reload ufw: %w", err))
		}
	}
	finalOutput, err := runUFW(ctx, runner, 10*time.Second, "status")
	if err != nil {
		return FirewallResult{}, rollback(fmt.Errorf("verify ufw status: %w", err))
	}
	finalState, err := parseUFWStatus(finalOutput)
	if err != nil {
		return FirewallResult{}, rollback(fmt.Errorf("parse final ufw status: %w", err))
	}
	if !finalState.Enabled && (initial.Enabled || len(desired) > 0) {
		return FirewallResult{}, rollback(errors.New("ufw remained disabled after reconciliation"))
	}
	for _, rule := range desired {
		if finalState.Rules[rule.target] != rule.comment {
			return FirewallResult{}, rollback(fmt.Errorf("firewall rule %s was not durably reconciled", rule.id))
		}
	}
	for _, target := range staleTargets {
		if _, exists := finalState.Rules[target]; exists {
			return FirewallResult{}, rollback(fmt.Errorf("stale Veil firewall rule %s remains", target))
		}
	}
	return FirewallResult{AppliedRuleIDs: append([]string(nil), request.RuleIDs...)}, nil
}

func parseDesiredUFWRules(request ResolvedFirewall) ([]ufwDesiredRule, error) {
	if len(request.Rules) != len(request.RuleIDs) {
		return nil, errors.New("firewall request must contain matching rule IDs and commands")
	}
	rules := make([]ufwDesiredRule, 0, len(request.Rules))
	seen := make(map[string]struct{}, len(request.Rules))
	for index, command := range request.Rules {
		if command.Command != "ufw" || len(command.Args) < 2 || command.Args[0] != "allow" {
			return nil, fmt.Errorf("firewall rule %s is not a supported ufw allow command", request.RuleIDs[index])
		}
		target := command.Args[1]
		comment := ""
		for i := 2; i+1 < len(command.Args); i++ {
			if command.Args[i] == "comment" {
				comment = command.Args[i+1]
				break
			}
		}
		if target == "" || !isVeilManagedFirewallComment(comment) {
			return nil, fmt.Errorf("firewall rule %s requires a Veil-managed comment", request.RuleIDs[index])
		}
		if _, exists := seen[target]; exists {
			return nil, fmt.Errorf("duplicate firewall target %s", target)
		}
		seen[target] = struct{}{}
		rules = append(rules, ufwDesiredRule{id: request.RuleIDs[index], target: target, comment: comment, args: append([]string(nil), command.Args...)})
	}
	return rules, nil
}

func containsManagementAccessRule(rules []ufwDesiredRule) bool {
	for _, rule := range rules {
		id := strings.ToLower(rule.id)
		comment := strings.ToLower(rule.comment)
		if strings.Contains(id, "management") || strings.Contains(id, "panel") || strings.Contains(comment, "management ssh") || strings.Contains(comment, "panel") {
			return true
		}
	}
	return false
}

func hasExistingManagementAccess(state ufwState) bool {
	for target, comment := range state.Rules {
		lowerTarget := strings.ToLower(target)
		lowerComment := strings.ToLower(comment)
		if strings.HasPrefix(lowerTarget, "22/") || strings.Contains(lowerComment, "openssh") ||
			strings.Contains(lowerComment, "management ssh") || strings.Contains(lowerComment, "panel") {
			return true
		}
	}
	return false
}

func restoreUFWState(ctx context.Context, runner CommandRunner, initial ufwState, desired []ufwDesiredRule) error {
	var joined error

	for _, rule := range desired {
		if _, err := runUFW(ctx, runner, 10*time.Second, "delete", "allow", rule.target); err != nil {
			joined = errors.Join(joined, err)
		}
	}
	if len(initial.Entries) > 0 {
		entries := append([]ufwRuleState(nil), initial.Entries...)
		sort.Slice(entries, func(i, j int) bool { return entries[i].Order < entries[j].Order })
		for _, entry := range entries {
			args, skip, err := ufwReplayArgs(entry)
			if skip {
				continue
			}
			if err != nil {
				joined = errors.Join(joined, err)
				continue
			}
			if _, err := runUFW(ctx, runner, 10*time.Second, args...); err != nil {
				joined = errors.Join(joined, err)
			}
		}
	} else {
		targets := make([]string, 0, len(initial.Rules))
		for target := range initial.Rules {
			targets = append(targets, target)
		}
		sort.Strings(targets)
		for _, target := range targets {
			comment := initial.Rules[target]
			args := []string{"allow", target}
			if comment != "" {
				args = append(args, "comment", comment)
			}
			if _, err := runUFW(ctx, runner, 10*time.Second, args...); err != nil {
				joined = errors.Join(joined, err)
			}
		}
	}

	if initial.Enabled {
		if _, err := runUFW(ctx, runner, 20*time.Second, "--force", "enable"); err != nil {
			joined = errors.Join(joined, err)
		}
	} else {
		if _, err := runUFW(ctx, runner, 20*time.Second, "--force", "disable"); err != nil {
			joined = errors.Join(joined, err)
		}
	}
	return joined
}

func runUFW(ctx context.Context, runner CommandRunner, timeout time.Duration, args ...string) (string, error) {
	command := []string{"env", "LC_ALL=C", "LANG=C", "ufw"}
	command = append(command, args...)
	return runner(ctx, command, timeout)
}

func parseUFWStatus(output string) (ufwState, error) {
	state := ufwState{Rules: make(map[string]string)}
	lines := strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n")
	if len(lines) == 0 {
		return state, errors.New("empty ufw status")
	}
	statusKnown := false
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "status:") || strings.HasPrefix(lower, "состояние:") {
			statusKnown = true
			inactive := strings.Contains(lower, "inactive") || strings.Contains(lower, "disabled") || strings.Contains(lower, "выключ")
			active := strings.Contains(lower, "active") || strings.Contains(lower, "enabled") || strings.Contains(lower, "включ")
			state.Enabled = active && !inactive
			continue
		}
		if strings.HasPrefix(lower, "to ") || strings.HasPrefix(lower, "--") {
			continue
		}
		parts := strings.SplitN(line, "#", 2)
		fields := strings.Fields(parts[0])
		if len(fields) < 2 {
			continue
		}
		comment := ""
		if len(parts) == 2 {
			comment = strings.TrimSpace(parts[1])
		}
		family := "ipv4"
		compact := make([]string, 0, len(fields))
		for _, field := range fields {
			if field == "(v6)" {
				family = "ipv6"
				continue
			}
			compact = append(compact, field)
		}
		actionIdx := -1
		for i, field := range compact {
			if isUFWAction(field) {
				actionIdx = i
				break
			}
		}
		if actionIdx < 0 {
			continue
		}
		action := strings.ToLower(compact[actionIdx])
		direction := "in"
		sourceIdx := actionIdx + 1
		if sourceIdx < len(compact) {
			switch strings.ToLower(compact[sourceIdx]) {
			case "in", "out":
				direction = strings.ToLower(compact[sourceIdx])
				sourceIdx++
			}
		}
		destination, iface := parseUFWToColumn(compact[:actionIdx])
		source, fromIface := parseUFWFromColumn(compact[sourceIdx:])
		if iface == "" {
			iface = fromIface
		}
		if destination == "" && iface == "" {
			continue
		}
		protocol := ""
		if slash := strings.LastIndex(destination, "/"); slash >= 0 && slash+1 < len(destination) {
			protocol = destination[slash+1:]
		}
		entry := ufwRuleState{
			Family: family, Action: action, Direction: direction, Interface: iface,
			Source: source, Destination: destination, Protocol: protocol,
			Order: len(state.Entries) + 1, Comment: comment,
		}
		entry.Args = buildUFWRestoreArgs(entry)
		if len(entry.Args) < 2 {
			continue
		}
		state.Entries = append(state.Entries, entry)
		if destination != "" {
			state.Rules[destination] = comment
		}
	}
	for i := range state.Entries {
		state.Entries[i].Enabled = state.Enabled
	}
	if !statusKnown {
		return state, errors.New("ufw status did not contain a machine-locale status line")
	}
	return state, nil
}

func isVeilManagedFirewallComment(comment string) bool {
	return strings.HasPrefix(strings.TrimSpace(comment), "Veil ")
}

func isProtectedVeilFirewallComment(comment string) bool {
	c := strings.TrimSpace(comment)
	return c == "Veil management SSH" || strings.HasPrefix(c, "Veil ACME")
}

func isUFWAction(action string) bool {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "allow", "deny", "reject", "limit":
		return true
	default:
		return false
	}
}

func parseUFWToColumn(fields []string) (destination, iface string) {
	if len(fields) >= 2 && strings.EqualFold(fields[len(fields)-2], "on") {
		return strings.Join(fields[:len(fields)-2], " "), fields[len(fields)-1]
	}
	return strings.Join(fields, " "), ""
}

func parseUFWFromColumn(fields []string) (source, iface string) {
	if len(fields) == 0 {
		return "Anywhere", ""
	}
	if len(fields) >= 2 && strings.EqualFold(fields[len(fields)-2], "on") {
		src := strings.Join(fields[:len(fields)-2], " ")
		if src == "" {
			src = "Anywhere"
		}
		return src, fields[len(fields)-1]
	}
	return strings.Join(fields, " "), ""
}

func ufwReplayArgs(entry ufwRuleState) ([]string, bool, error) {
	if !isUFWAction(entry.Action) {
		return nil, false, errors.New("firewall recovery entry is incomplete")
	}
	if strings.EqualFold(entry.Family, "ipv6") && isUnrestrictedUFWSource(entry.Source) {
		// Simple IPv4 restore (`ufw allow 22/tcp`) already installs the IPv6
		// twin. Replaying "(v6)" status lines as ufw arguments is invalid.
		return nil, true, nil
	}
	args := buildUFWRestoreArgs(entry)
	if len(args) < 2 {
		return nil, false, errors.New("firewall recovery entry is incomplete")
	}
	return args, false, nil
}

func buildUFWRestoreArgs(entry ufwRuleState) []string {
	action := strings.ToLower(strings.TrimSpace(entry.Action))
	if !isUFWAction(action) {
		return nil
	}
	dest := strings.TrimSpace(entry.Destination)
	iface := strings.TrimSpace(entry.Interface)
	unrestricted := isUnrestrictedUFWSource(entry.Source)
	direction := strings.ToLower(strings.TrimSpace(entry.Direction))

	// Unrestricted inbound rules without an interface keep simple syntax so
	// UFW still installs the matching IPv6 twin.
	if unrestricted && iface == "" && direction != "out" && dest != "" && !isUnrestrictedUFWDestination(dest) {
		return appendUFWComment([]string{action, dest}, entry.Comment)
	}

	args := []string{action}
	switch direction {
	case "out":
		args = append(args, "out")
	default:
		if iface != "" {
			args = append(args, "in")
		}
	}
	if iface != "" {
		args = append(args, "on", iface)
	}
	if !unrestricted {
		from := ufwSourceAddress(entry.Source)
		if from == "" {
			return nil
		}
		args = append(args, "from", from)
	}
	port, proto := splitUFWDestination(dest)
	switch {
	case port != "" || proto != "":
		args = append(args, "to", "any")
		if port != "" {
			args = append(args, "port", port)
		}
		if proto != "" {
			args = append(args, "proto", proto)
		}
	case dest != "" && !isUnrestrictedUFWDestination(dest):
		args = append(args, "to", dest)
	}
	if len(args) < 2 {
		return nil
	}
	return appendUFWComment(args, entry.Comment)
}

func appendUFWComment(args []string, comment string) []string {
	if strings.TrimSpace(comment) == "" {
		return args
	}
	return append(args, "comment", comment)
}

func isUnrestrictedUFWSource(source string) bool {
	return isUnrestrictedUFWAddress(source)
}

func isUnrestrictedUFWDestination(dest string) bool {
	return isUnrestrictedUFWAddress(dest)
}

func isUnrestrictedUFWAddress(value string) bool {
	s := strings.ToLower(strings.TrimSpace(value))
	s = strings.TrimSpace(strings.TrimSuffix(s, "(v6)"))
	switch s {
	case "", "anywhere", "any", "::/0", "0.0.0.0/0":
		return true
	default:
		return false
	}
}

func ufwSourceAddress(source string) string {
	fields := strings.Fields(strings.TrimSpace(source))
	if len(fields) == 0 || isUnrestrictedUFWSource(fields[0]) {
		return ""
	}
	return fields[0]
}

func splitUFWDestination(dest string) (port, proto string) {
	dest = strings.TrimSpace(dest)
	if dest == "" || isUnrestrictedUFWDestination(dest) {
		return "", ""
	}
	if slash := strings.LastIndex(dest, "/"); slash >= 0 && slash+1 < len(dest) {
		candidate := strings.ToLower(dest[slash+1:])
		switch candidate {
		case "tcp", "udp", "icmp", "ipv6", "esp", "ah", "igmp", "gre", "vrrp":
			return dest[:slash], candidate
		}
	}
	if isUFWPortSpec(dest) {
		return dest, ""
	}
	return "", ""
}

func isUFWPortSpec(value string) bool {
	if value == "" || value[0] < '0' || value[0] > '9' {
		return false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c >= '0' && c <= '9' || c == ',' || c == ':' {
			continue
		}
		return false
	}
	return true
}
