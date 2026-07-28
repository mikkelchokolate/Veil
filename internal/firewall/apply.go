package firewall

import (
	"fmt"
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
