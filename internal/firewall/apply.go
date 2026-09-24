package firewall

import (
	"fmt"
	"sort"
	"strings"
	"time"

	veilruntime "github.com/mikkelchokolate/Veil/internal/runtime"
)

// UFWApplier runs ufw commands to open ports required by Veil services.
type UFWApplier struct {
	runner RuntimeCommandRunner
}

// NewUFWApplier creates a UFW applier using the default command runner.
func NewUFWApplier() UFWApplier {
	return UFWApplier{runner: veilruntime.NewRuntimeCommandExecutor()}
}

// NewUFWApplierWithRunner creates a UFW applier with the provided runner (used in tests).
func NewUFWApplierWithRunner(runner RuntimeCommandRunner) UFWApplier {
	if runner == nil {
		runner = veilruntime.NewRuntimeCommandExecutor()
	}
	return UFWApplier{runner: runner}
}

// EnsureActive enables ufw if it is installed but not active.
func (a UFWApplier) EnsureActive() error {
	status := NewStatusReader(a.runner).Active
	active, err := status()
	if err != nil {
		return fmt.Errorf("read ufw status: %w", err)
	}
	if active {
		return nil
	}
	out := a.runner.Run(veilruntime.RuntimeCommandInput{
		Command: []string{"ufw", "--force", "enable"},
		Timeout: 15 * time.Second,
		Env:     stableUFWEnv,
	})
	if out.Err != nil {
		return fmt.Errorf("enable ufw: %w (output: %s)", out.Err, out.Output)
	}
	return nil
}

// ApplyRules adds the requested ufw allow rules, skipping duplicates, and
// reloads ufw so the on-disk rules are synced into the live kernel ruleset.
func (a UFWApplier) ApplyRules(rules []Rule) error {
	applied := false
	for _, rule := range rules {
		if rule.Command != "ufw" {
			return fmt.Errorf("unsupported firewall command %q", rule.Command)
		}
		out := a.runner.Run(veilruntime.RuntimeCommandInput{
			Command: append([]string{"ufw"}, rule.Args...),
			Timeout: 15 * time.Second,
			Env:     stableUFWEnv,
		})
		// ufw returns exit code 1 when the rule already exists; treat that as success.
		if out.Err != nil && !isUFWDuplicateRule(out.Output) {
			return fmt.Errorf("ufw %v: %w (output: %s)", rule.Args, out.Err, out.Output)
		}
		applied = true
	}
	if applied {
		out := a.runner.Run(veilruntime.RuntimeCommandInput{
			Command: []string{"ufw", "reload"},
			Timeout: 15 * time.Second,
			Env:     stableUFWEnv,
		})
		if out.Err != nil {
			return fmt.Errorf("reload ufw: %w (output: %s)", out.Err, out.Output)
		}
	}
	return nil
}

func isUFWDuplicateRule(output string) bool {
	return strings.Contains(output, "Skipping adding existing rule") || strings.Contains(output, "already exists")
}

// IsVeilManagedComment reports whether a ufw rule comment marks the rule as
// Veil-managed — every comment Veil stages carries the "Veil " prefix. The
// privileged reconcile uses the same predicate, so the local and helper paths
// agree on which rules are safe to prune.
func IsVeilManagedComment(comment string) bool {
	return strings.HasPrefix(strings.TrimSpace(comment), "Veil ")
}

// IsProtectedVeilComment reports whether a Veil-managed rule must never be
// pruned as stale: the management SSH allow keeps the operator's channel
// open, and ACME challenge rules guard certificate issuance.
func IsProtectedVeilComment(comment string) bool {
	c := strings.TrimSpace(comment)
	return c == "Veil management SSH" || strings.HasPrefix(c, "Veil ACME")
}

// PruneStaleManagedRules deletes Veil-managed allow rules that are not in the
// desired set — the local counterpart of the privileged reconcile's stale
// pass. It runs for any desired set, including an empty one, so clearing
// every managed port cannot strand stale UFW allows on hosts without the
// privileged helper. It never enables UFW: with no desired rules there is no
// management-access evidence that enabling would be safe. Returns the number
// of rules deleted.
func (a UFWApplier) PruneStaleManagedRules(desired []Rule) (int, error) {
	keep := make(map[string]struct{}, len(desired))
	for _, rule := range desired {
		if rule.Command == "ufw" && len(rule.Args) >= 2 && rule.Args[0] == "allow" {
			keep[rule.Args[1]] = struct{}{}
		}
	}
	snap, err := a.snapshot()
	if err != nil {
		return 0, err
	}
	stale := make([]string, 0)
	for target, comment := range snap.Rules {
		if !IsVeilManagedComment(comment) || IsProtectedVeilComment(comment) {
			continue
		}
		if _, ok := keep[target]; !ok {
			stale = append(stale, target)
		}
	}
	sort.Strings(stale)
	for _, target := range stale {
		out := a.runUFW(15*time.Second, "delete", "allow", target)
		if out.Err != nil {
			return 0, fmt.Errorf("delete stale Veil firewall rule %s: %w (output: %s)", target, out.Err, out.Output)
		}
	}
	// Deletions need a reload to reach the live ruleset while UFW is active —
	// the same contract ApplyRules uses after staging allows.
	if len(stale) > 0 && snap.Active {
		out := a.runUFW(15*time.Second, "reload")
		if out.Err != nil {
			return 0, fmt.Errorf("reload ufw after pruning stale rules: %w (output: %s)", out.Err, out.Output)
		}
	}
	return len(stale), nil
}

// UFWRulesFromResponses converts display-oriented rule responses into ufw commands.
func UFWRulesFromResponses(responses []RuleResponse) []Rule {
	rules := make([]Rule, 0, len(responses))
	for _, r := range responses {
		if r.Port <= 0 || (r.Protocol != "tcp" && r.Protocol != "udp") {
			continue
		}
		rules = append(rules, Rule{
			Command: "ufw",
			Args:    []string{"allow", fmt.Sprintf("%d/%s", r.Port, r.Protocol), "comment", r.Service},
		})
	}
	return rules
}
