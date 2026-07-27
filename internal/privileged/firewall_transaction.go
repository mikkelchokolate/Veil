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
	enabled bool
	rules   map[string]string
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
	if !containsManagementAccessRule(desired) && !hasExistingManagementAccess(initial) {
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
	for target, comment := range initial.rules {
		if !isVeilManagedFirewallComment(comment) {
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
	if !initial.enabled {
		if _, err := runUFW(ctx, runner, 20*time.Second, "--force", "enable"); err != nil {
			return FirewallResult{}, rollback(fmt.Errorf("enable ufw: %w", err))
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
	if !finalState.enabled {
		return FirewallResult{}, rollback(errors.New("ufw remained disabled after reconciliation"))
	}
	for _, rule := range desired {
		if finalState.rules[rule.target] != rule.comment {
			return FirewallResult{}, rollback(fmt.Errorf("firewall rule %s was not durably reconciled", rule.id))
		}
	}
	for _, target := range staleTargets {
		if _, exists := finalState.rules[target]; exists {
			return FirewallResult{}, rollback(fmt.Errorf("stale Veil firewall rule %s remains", target))
		}
	}
	return FirewallResult{AppliedRuleIDs: append([]string(nil), request.RuleIDs...)}, nil
}

func parseDesiredUFWRules(request ResolvedFirewall) ([]ufwDesiredRule, error) {
	if len(request.Rules) == 0 || len(request.Rules) != len(request.RuleIDs) {
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
	for target, comment := range state.rules {
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
	targets := make([]string, 0, len(initial.rules))
	for target := range initial.rules {
		targets = append(targets, target)
	}
	sort.Strings(targets)
	for _, target := range targets {
		comment := initial.rules[target]
		args := []string{"allow", target}
		if comment != "" {
			args = append(args, "comment", comment)
		}
		if _, err := runUFW(ctx, runner, 10*time.Second, args...); err != nil {
			joined = errors.Join(joined, err)
		}
	}

	if initial.enabled {
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
	state := ufwState{rules: make(map[string]string)}
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
			state.enabled = active && !inactive
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
		target := fields[0]
		comment := ""
		if len(parts) == 2 {
			comment = strings.TrimSpace(parts[1])
		}
		state.rules[target] = comment
	}
	if !statusKnown {
		return state, errors.New("ufw status did not contain a machine-locale status line")
	}
	return state, nil
}

func isVeilManagedFirewallComment(comment string) bool {
	return strings.HasPrefix(strings.TrimSpace(comment), "Veil ")
}
