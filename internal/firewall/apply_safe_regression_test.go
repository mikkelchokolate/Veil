package firewall

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	veilruntime "github.com/mikkelchokolate/Veil/internal/runtime"
)

type safeUFWModel struct {
	enabled   bool
	rules     map[string]string
	calls     [][]string
	mutations []string
	failAt    int
}

func (m *safeUFWModel) Run(input veilruntime.RuntimeCommandInput) veilruntime.RuntimeCommandOutput {
	m.calls = append(m.calls, append([]string(nil), input.Command...))
	call := len(m.calls)
	if m.failAt == call {
		return veilruntime.RuntimeCommandOutput{Err: errors.New("injected ufw failure"), Output: "injected ufw failure"}
	}
	command := input.Command
	if len(command) < 2 || command[0] != "ufw" {
		return veilruntime.RuntimeCommandOutput{Err: fmt.Errorf("unexpected command %v", command)}
	}
	switch command[1] {
	case "status":
		return veilruntime.RuntimeCommandOutput{Output: m.statusOutput()}
	case "show":
		return veilruntime.RuntimeCommandOutput{Output: m.showAddedOutput()}
	case "--dry-run":
		return veilruntime.RuntimeCommandOutput{Output: "dry-run ok"}
	case "--force":
		if len(command) >= 3 && command[2] == "enable" {
			m.enabled = true
			m.mutations = append(m.mutations, "enable")
			return veilruntime.RuntimeCommandOutput{}
		}
		if len(command) >= 3 && command[2] == "disable" {
			m.enabled = false
			m.mutations = append(m.mutations, "disable")
			return veilruntime.RuntimeCommandOutput{}
		}
	case "allow":
		if len(command) < 3 {
			return veilruntime.RuntimeCommandOutput{Err: errors.New("missing allow target")}
		}
		comment := ruleComment(command[2:])
		m.rules[command[2]] = comment
		m.mutations = append(m.mutations, "allow "+command[2])
		return veilruntime.RuntimeCommandOutput{}
	case "delete":
		if len(command) >= 4 && command[2] == "allow" {
			delete(m.rules, command[3])
			m.mutations = append(m.mutations, "delete "+command[3])
			return veilruntime.RuntimeCommandOutput{}
		}
	case "reload":
		m.mutations = append(m.mutations, "reload")
		return veilruntime.RuntimeCommandOutput{}
	}
	return veilruntime.RuntimeCommandOutput{Err: fmt.Errorf("unsupported fake ufw command %v", command)}
}

func (m *safeUFWModel) statusOutput() string {
	var b strings.Builder
	if m.enabled {
		b.WriteString("Status: active\n")
	} else {
		b.WriteString("Status: inactive\n")
	}
	targets := make([]string, 0, len(m.rules))
	for target := range m.rules {
		targets = append(targets, target)
	}
	sort.Strings(targets)
	for _, target := range targets {
		fmt.Fprintf(&b, "%s ALLOW Anywhere # %s\n", target, m.rules[target])
	}
	return b.String()
}

func (m *safeUFWModel) showAddedOutput() string {
	var b strings.Builder
	b.WriteString("Added user rules (see 'ufw status' for running firewall):\n")
	targets := make([]string, 0, len(m.rules))
	for target := range m.rules {
		targets = append(targets, target)
	}
	sort.Strings(targets)
	for _, target := range targets {
		comment := m.rules[target]
		if comment == "" {
			fmt.Fprintf(&b, "ufw allow %s\n", target)
			continue
		}
		fmt.Fprintf(&b, "ufw allow %s comment %s\n", target, comment)
	}
	return b.String()
}

func managementSSHAndPanelRules() []Rule {
	return []Rule{
		{Command: "ufw", Args: []string{"allow", "22/tcp", "comment", "Veil management SSH"}},
		{Command: "ufw", Args: []string{"allow", "2096/tcp", "comment", "Veil panel"}},
	}
}

func TestApplySafelyStagesManagementAccessBeforeEnable(t *testing.T) {
	model := &safeUFWModel{rules: map[string]string{}}
	if err := NewUFWApplierWithRunner(model).ApplySafely(managementSSHAndPanelRules()); err != nil {
		t.Fatal(err)
	}
	if len(model.mutations) == 0 || model.mutations[0] == "enable" {
		t.Fatalf("ufw was enabled before management access was staged: %v", model.mutations)
	}
	if !model.enabled {
		t.Fatal("successful apply left ufw inactive")
	}
	enableAt := -1
	for i, mutation := range model.mutations {
		if mutation == "enable" {
			enableAt = i
			break
		}
	}
	if enableAt < 0 {
		t.Fatalf("ufw was never enabled: %v", model.mutations)
	}
	for i := 0; i < enableAt; i++ {
		if !strings.HasPrefix(model.mutations[i], "allow ") {
			t.Fatalf("non-allow mutation %q preceded enable: %v", model.mutations[i], model.mutations)
		}
	}
	if _, ok := model.rules["22/tcp"]; !ok {
		t.Fatalf("SSH rule was not staged: %v", model.rules)
	}
}

func TestApplySafelyUsesCustomSSHPortBeforeEnable(t *testing.T) {
	model := &safeUFWModel{rules: map[string]string{}}
	rules := []Rule{
		{Command: "ufw", Args: []string{"allow", "2222/tcp", "comment", "Veil management SSH"}},
		{Command: "ufw", Args: []string{"allow", "443/tcp", "comment", "Veil panel HTTPS"}},
	}
	if err := NewUFWApplierWithRunner(model).ApplySafely(rules); err != nil {
		t.Fatal(err)
	}
	if len(model.mutations) == 0 || model.mutations[0] != "allow 2222/tcp" {
		t.Fatalf("custom SSH port was not staged first: %v", model.mutations)
	}
	if _, ok := model.rules["2222/tcp"]; !ok {
		t.Fatalf("custom SSH rule missing: %v", model.rules)
	}
}

func TestApplySafelyRefusesToEnableWithoutSSHAccess(t *testing.T) {
	model := &safeUFWModel{rules: map[string]string{}}
	rules := []Rule{{Command: "ufw", Args: []string{"allow", "2096/tcp", "comment", "Veil panel"}}}
	if err := NewUFWApplierWithRunner(model).ApplySafely(rules); err == nil {
		t.Fatal("inactive ufw was enabled without an SSH management rule")
	}
	if model.enabled {
		t.Fatal("management lockout preflight failure still enabled ufw")
	}
	for _, mutation := range model.mutations {
		if mutation == "enable" {
			t.Fatalf("enable ran without SSH access: %v", model.mutations)
		}
	}
}

func TestApplySafelyFailureAtEveryCommandRestoresRulesAndEnabledState(t *testing.T) {
	initial := map[string]string{"9999/tcp": "other"}
	success := &safeUFWModel{rules: cloneRuleMap(initial)}
	if err := NewUFWApplierWithRunner(success).ApplySafely(managementSSHAndPanelRules()); err != nil {
		t.Fatal(err)
	}
	if len(success.calls) == 0 {
		t.Fatal("expected ufw commands on the success path")
	}
	for failAt := 1; failAt <= len(success.calls); failAt++ {
		t.Run(fmt.Sprintf("command-%d", failAt), func(t *testing.T) {
			model := &safeUFWModel{rules: cloneRuleMap(initial), failAt: failAt}
			if err := NewUFWApplierWithRunner(model).ApplySafely(managementSSHAndPanelRules()); err == nil {
				t.Fatalf("expected failure at ufw command %d; calls=%v", failAt, model.calls)
			}
			if model.enabled {
				t.Errorf("failure at command %d did not restore disabled state; mutations=%v", failAt, model.mutations)
			}
			if !reflect.DeepEqual(model.rules, initial) {
				t.Errorf("failure at command %d left rules=%v want=%v mutations=%v", failAt, model.rules, initial, model.mutations)
			}
		})
	}
}

func TestApplySafelySkipsEnableWhenAlreadyActive(t *testing.T) {
	model := &safeUFWModel{
		enabled: true,
		rules:   map[string]string{"22/tcp": "OpenSSH"},
	}
	if err := NewUFWApplierWithRunner(model).ApplySafely(managementSSHAndPanelRules()); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range model.mutations {
		if mutation == "enable" {
			t.Fatalf("already-active ufw was force-enabled: %v", model.mutations)
		}
	}
}

func cloneRuleMap(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
