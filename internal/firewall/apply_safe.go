package firewall

import (
	"errors"
	"fmt"
	"strings"
	"time"

	veilruntime "github.com/mikkelchokolate/Veil/internal/runtime"
)

type ufwSnapshot struct {
	Active bool
	Rules  map[string]string
	// Allows records only the inbound ALLOW entries (destination → comment).
	// A deny/reject/limit on the SSH port does not pass management traffic,
	// so it must not satisfy the enable gate (#1007).
	Allows map[string]string
}

// ApplySafely stages every requested allow rule, including SSH management
// access, before enabling a previously inactive firewall. On any rule,
// enable, or reload failure it restores the previous enabled state and
// removes rules added by this call.
func (a UFWApplier) ApplySafely(rules []Rule) error {
	if len(rules) == 0 {
		return nil
	}
	for _, rule := range rules {
		if rule.Command != "ufw" {
			return fmt.Errorf("unsupported firewall command %q", rule.Command)
		}
		if len(rule.Args) < 2 || rule.Args[0] != "allow" {
			return fmt.Errorf("unsupported firewall rule %v", rule.Args)
		}
	}

	if err := EnsureIPv6Managed(); err != nil {
		return err
	}

	initial, err := a.snapshot()
	if err != nil {
		return err
	}
	if !initial.Active && !containsSSHManagementRule(rules) && !snapshotHasSSH(initial) {
		return errors.New("refusing to enable UFW without a staged SSH management access rule")
	}

	for _, rule := range rules {
		out := a.runUFW(15*time.Second, append([]string{"--dry-run"}, rule.Args...)...)
		if out.Err != nil {
			return fmt.Errorf("dry-run ufw %v: %w (output: %s)", rule.Args, out.Err, out.Output)
		}
	}

	added := make([]string, 0, len(rules))
	rollback := func(cause error) error {
		if restoreErr := a.restore(initial, added); restoreErr != nil {
			return fmt.Errorf("%v; restore previous UFW state: %w", cause, restoreErr)
		}
		return cause
	}

	for _, rule := range rules {
		target := rule.Args[1]
		_, existed := initial.Rules[target]
		out := a.runUFW(15*time.Second, rule.Args...)
		if out.Err != nil && !isUFWDuplicateRule(out.Output) {
			return rollback(fmt.Errorf("ufw %v: %w (output: %s)", rule.Args, out.Err, out.Output))
		}
		if !existed && !isUFWDuplicateRule(out.Output) {
			added = append(added, target)
		}
	}

	if !initial.Active {
		out := a.runUFW(15*time.Second, "--force", "enable")
		if out.Err != nil {
			return rollback(fmt.Errorf("enable ufw: %w (output: %s)", out.Err, out.Output))
		}
	} else {
		out := a.runUFW(15*time.Second, "reload")
		if out.Err != nil {
			return rollback(fmt.Errorf("reload ufw: %w (output: %s)", out.Err, out.Output))
		}
	}

	final, err := a.snapshot()
	if err != nil {
		return rollback(err)
	}
	if !final.Active {
		return rollback(errors.New("ufw remained disabled after applying rules"))
	}
	return nil
}

func (a UFWApplier) snapshot() (ufwSnapshot, error) {
	out := a.runUFW(15*time.Second, "status")
	if out.Err != nil {
		return ufwSnapshot{}, fmt.Errorf("read ufw status: %w (output: %s)", out.Err, out.Output)
	}
	snap, err := parseUFWStatus(out.Output)
	if err != nil {
		return ufwSnapshot{}, fmt.Errorf("parse ufw status: %w", err)
	}
	return snap, nil
}

func (a UFWApplier) restore(initial ufwSnapshot, added []string) error {
	var joined error
	for i := len(added) - 1; i >= 0; i-- {
		out := a.runUFW(15*time.Second, "delete", "allow", added[i])
		if out.Err != nil && !isUFWDuplicateRule(out.Output) {
			joined = errors.Join(joined, fmt.Errorf("delete %s: %w (output: %s)", added[i], out.Err, out.Output))
		}
	}
	if initial.Active {
		out := a.runUFW(15*time.Second, "--force", "enable")
		if out.Err != nil {
			joined = errors.Join(joined, fmt.Errorf("restore enable: %w (output: %s)", out.Err, out.Output))
		}
	} else {
		out := a.runUFW(15*time.Second, "--force", "disable")
		if out.Err != nil {
			joined = errors.Join(joined, fmt.Errorf("restore disable: %w (output: %s)", out.Err, out.Output))
		}
	}
	return joined
}

func (a UFWApplier) runUFW(timeout time.Duration, args ...string) veilruntime.RuntimeCommandOutput {
	return a.runner.Run(veilruntime.RuntimeCommandInput{
		Command: append([]string{"ufw"}, args...),
		Timeout: timeout,
		Env:     stableUFWEnv,
	})
}

func parseUFWStatus(output string) (ufwSnapshot, error) {
	snap := ufwSnapshot{Rules: map[string]string{}, Allows: map[string]string{}}
	statusKnown := false
	for _, raw := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "status:") || strings.HasPrefix(lower, "состояние:") {
			statusKnown = true
			inactive := strings.Contains(lower, "inactive") || strings.Contains(lower, "disabled") || strings.Contains(lower, "выключ")
			active := strings.Contains(lower, "active") || strings.Contains(lower, "enabled") || strings.Contains(lower, "включ")
			snap.Active = active && !inactive
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
		snap.Rules[fields[0]] = comment
		actionIdx := -1
		for i, field := range fields {
			if isUFWStatusAction(field) {
				actionIdx = i
				break
			}
		}
		if actionIdx < 1 {
			continue
		}
		direction := "in"
		if actionIdx+1 < len(fields) {
			switch d := strings.ToLower(fields[actionIdx+1]); d {
			case "in", "out":
				direction = d
			}
		}
		if strings.EqualFold(fields[actionIdx], "allow") && direction != "out" {
			snap.Allows[strings.Join(fields[:actionIdx], " ")] = comment
		}
	}
	if !statusKnown {
		return snap, errors.New("ufw status did not contain a status line")
	}
	return snap, nil
}

// isUFWStatusAction reports whether a status field is a ufw rule action
// (ALLOW/DENY/REJECT/LIMIT), matching the privileged reconcile's action set.
func isUFWStatusAction(field string) bool {
	switch strings.ToLower(strings.TrimSpace(field)) {
	case "allow", "deny", "reject", "limit":
		return true
	default:
		return false
	}
}

func containsSSHManagementRule(rules []Rule) bool {
	for _, rule := range rules {
		if isSSHManagementRule(rule.Args) {
			return true
		}
	}
	return false
}

func isSSHManagementRule(args []string) bool {
	return strings.Contains(strings.ToLower(strings.Join(args, " ")), "ssh")
}

func snapshotHasSSH(snap ufwSnapshot) bool {
	for target, comment := range snap.Allows {
		if strings.Contains(strings.ToLower(comment), "ssh") {
			return true
		}
		if strings.HasPrefix(strings.ToLower(target), "22/") {
			return true
		}
	}
	return false
}

func ruleComment(args []string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "comment" {
			return strings.Trim(strings.Join(args[i+1:], " "), `"'`)
		}
	}
	return ""
}
