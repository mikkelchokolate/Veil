package privileged

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"testing"
	"time"
)

type transactionalUFWModel struct {
	enabled   bool
	rules     map[string]string
	calls     [][]string
	mutations []string
	failAt    int
	localized bool
}

func (m *transactionalUFWModel) runner(_ context.Context, command []string, _ time.Duration) (string, error) {
	m.calls = append(m.calls, append([]string(nil), command...))
	call := len(m.calls)
	if m.failAt == call {
		return "injected ufw failure", errors.New("injected ufw failure")
	}
	if len(command) < 2 || command[0] != "ufw" {
		return "", fmt.Errorf("unexpected command %v", command)
	}
	switch command[1] {
	case "status":
		if m.localized {
			if m.enabled {
				return "Состояние: включён", nil
			}
			return "Состояние: выключен", nil
		}
		if m.enabled {
			return "Status: active", nil
		}
		return "Status: inactive", nil
	case "--force":
		if len(command) >= 3 && command[2] == "enable" {
			m.enabled = true
			m.mutations = append(m.mutations, "enable")
			return "", nil
		}
		if len(command) >= 3 && command[2] == "disable" {
			m.enabled = false
			m.mutations = append(m.mutations, "disable")
			return "", nil
		}
	case "allow":
		if len(command) < 3 {
			return "", errors.New("missing allow target")
		}
		comment := ""
		for i := 3; i+1 < len(command); i++ {
			if command[i] == "comment" {
				comment = command[i+1]
			}
		}
		m.rules[command[2]] = comment
		m.mutations = append(m.mutations, "allow "+command[2])
		return "", nil
	case "delete":
		if len(command) >= 4 && command[2] == "allow" {
			delete(m.rules, command[3])
			m.mutations = append(m.mutations, "delete "+command[3])
			return "", nil
		}
	case "reload":
		m.mutations = append(m.mutations, "reload")
		return "", nil
	}
	return "", fmt.Errorf("unsupported fake ufw command %v", command)
}

func TestFirewallFailureAtEveryCommandRestoresRulesAndEnabledState(t *testing.T) {
	for failAt := 1; failAt <= 5; failAt++ {
		t.Run(fmt.Sprintf("command-%d", failAt), func(t *testing.T) {
			initialRules := map[string]string{"22/tcp": "OpenSSH", "9999/tcp": "Veil stale"}
			model := &transactionalUFWModel{rules: cloneFirewallRules(initialRules), failAt: failAt}
			_, err := runFirewallRules(context.Background(), model.runner, transactionalFirewallRequest())
			if err == nil {
				t.Fatalf("expected failure at ufw command %d; calls=%v", failAt, model.calls)
			}
			if model.enabled {
				t.Errorf("failure at command %d did not restore disabled state", failAt)
			}
			if !reflect.DeepEqual(model.rules, initialRules) {
				t.Errorf("failure at command %d left rules=%v want=%v", failAt, model.rules, initialRules)
			}
		})
	}
}

func TestFirewallStagesManagementAccessBeforeEnableAndDeletesStaleManagedRules(t *testing.T) {
	model := &transactionalUFWModel{rules: map[string]string{
		"22/tcp":   "OpenSSH",
		"9999/tcp": "Veil stale",
	}}
	if _, err := runFirewallRules(context.Background(), model.runner, transactionalFirewallRequest()); err != nil {
		t.Fatal(err)
	}
	if len(model.mutations) == 0 || model.mutations[0] == "enable" {
		t.Fatalf("ufw was enabled before management access was staged: %v", model.mutations)
	}
	if _, exists := model.rules["9999/tcp"]; exists {
		t.Fatalf("stale Veil-managed rule was not reconciled away: %v", model.rules)
	}
	if !model.enabled {
		t.Fatal("successful reconciliation did not restore desired enabled state")
	}
}

func TestFirewallRefusesToEnableWithoutRequiredManagementAccess(t *testing.T) {
	model := &transactionalUFWModel{rules: map[string]string{}}
	request := ResolvedFirewall{Rules: []FirewallRule{{Command: "ufw", Args: []string{"allow", "4315/udp", "comment", "Veil Hysteria2"}}}}
	if _, err := runFirewallRules(context.Background(), model.runner, request); err == nil {
		t.Fatal("inactive ufw was enabled without SSH or Panel management access rule")
	}
	if model.enabled {
		t.Fatal("management lockout preflight failure still enabled ufw")
	}
}

func TestFirewallStatusDetectionIsLocaleIndependent(t *testing.T) {
	model := &transactionalUFWModel{enabled: true, localized: true, rules: map[string]string{"22/tcp": "OpenSSH"}}
	if _, err := runFirewallRules(context.Background(), model.runner, transactionalFirewallRequest()); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range model.mutations {
		if mutation == "enable" {
			t.Fatalf("localized active status was parsed as inactive; calls=%v", model.calls)
		}
	}
}

func transactionalFirewallRequest() ResolvedFirewall {
	return ResolvedFirewall{
		RuleIDs: []string{"management-ssh", "panel-https"},
		Rules: []FirewallRule{
			{Command: "ufw", Args: []string{"allow", "22/tcp", "comment", "Veil management SSH"}},
			{Command: "ufw", Args: []string{"allow", "443/tcp", "comment", "Veil Panel HTTPS"}},
		},
	}
}

func cloneFirewallRules(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
