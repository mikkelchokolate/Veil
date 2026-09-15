package privileged

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

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
	normalized := command
	if len(normalized) >= 4 && normalized[0] == "env" {
		normalized = normalized[3:]
	}
	if len(normalized) < 2 || normalized[0] != "ufw" {
		return "", fmt.Errorf("unexpected command %v", command)
	}
	command = normalized
	switch command[1] {
	case "status":
		var output strings.Builder
		if m.localized {
			if m.enabled {
				output.WriteString("Состояние: включён\n")
			} else {
				output.WriteString("Состояние: выключен\n")
			}
		} else if m.enabled {
			output.WriteString("Status: active\n")
		} else {
			output.WriteString("Status: inactive\n")
		}
		targets := make([]string, 0, len(m.rules))
		for target := range m.rules {
			targets = append(targets, target)
		}
		sort.Strings(targets)
		for _, target := range targets {
			fmt.Fprintf(&output, "%s ALLOW Anywhere # %s\n", target, m.rules[target])
		}
		return output.String(), nil
	case "--dry-run":
		return "dry-run ok", nil
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
	for failAt := 1; failAt <= 8; failAt++ {
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

func TestParseUFWStatusDoesNotTreatIPv6MarkerAsAction(t *testing.T) {
	state, err := parseUFWStatus(strings.Join([]string{
		"Status: active",
		"To                         Action      From",
		"--                         ------      ----",
		"22/tcp                     ALLOW       Anywhere                   # Veil management SSH",
		"443/tcp                    ALLOW       Anywhere                   # Veil panel HTTPS",
		"443/tcp (v6)               ALLOW       Anywhere (v6)              # Veil panel HTTPS",
		"22356/udp                  ALLOW       Anywhere                   # Veil Hysteria2",
		"22356/udp (v6)             ALLOW       Anywhere (v6)              # Veil Hysteria2",
	}, "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !state.Enabled {
		t.Fatal("expected active UFW")
	}
	var v6 int
	for _, entry := range state.Entries {
		if entry.Action == "(v6)" || (len(entry.Args) > 0 && entry.Args[0] == "(v6)") {
			t.Fatalf("parsed (v6) as ufw action: %+v", entry)
		}
		if !isUFWAction(entry.Action) {
			t.Fatalf("non-ufw action %q in %+v", entry.Action, entry)
		}
		if entry.Family == "ipv6" {
			v6++
		}
	}
	if v6 != 2 {
		t.Fatalf("ipv6 entries = %d, want 2", v6)
	}
	if got := state.Rules["443/tcp"]; got != "Veil panel HTTPS" {
		t.Fatalf("443/tcp comment = %q", got)
	}
}

func TestRestoreUFWStateSkipsIPv6StatusLines(t *testing.T) {
	model := &transactionalUFWModel{enabled: true, rules: map[string]string{}}
	initial, err := parseUFWStatus(strings.Join([]string{
		"Status: active",
		"22/tcp ALLOW Anywhere # Veil management SSH",
		"22/tcp (v6) ALLOW Anywhere (v6) # Veil management SSH",
		"443/tcp ALLOW Anywhere # Veil panel HTTPS",
		"443/tcp (v6) ALLOW Anywhere (v6) # Veil panel HTTPS",
	}, "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := restoreUFWState(context.Background(), model.runner, initial, nil); err != nil {
		t.Fatal(err)
	}
	for _, call := range model.calls {
		joined := strings.Join(call, " ")
		if strings.Contains(joined, "(v6)") {
			t.Fatalf("restore replayed IPv6 marker as ufw args: %v", call)
		}
	}
}

func TestParseUFWStatusCapturesSourceInterfaceAndFamily(t *testing.T) {
	state, err := parseUFWStatus(strings.Join([]string{
		"Status: active",
		"To                         Action      From",
		"--                         ------      ----",
		"22/tcp                     ALLOW       10.0.0.0/8",
		"22/tcp                     ALLOW       Anywhere",
		"22/tcp on eth0             ALLOW       Anywhere",
		"3000                       ALLOW       192.168.0.0/16",
		"22/tcp (v6)                ALLOW       2001:db8::/32",
		"22/tcp (v6)                ALLOW       Anywhere (v6)",
	}, "\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		source      string
		destination string
		iface       string
		family      string
	}{
		{source: "10.0.0.0/8", destination: "22/tcp", family: "ipv4"},
		{source: "Anywhere", destination: "22/tcp", family: "ipv4"},
		{source: "Anywhere", destination: "22/tcp", iface: "eth0", family: "ipv4"},
		{source: "192.168.0.0/16", destination: "3000", family: "ipv4"},
		{source: "2001:db8::/32", destination: "22/tcp", family: "ipv6"},
		{source: "Anywhere", destination: "22/tcp", family: "ipv6"},
	}
	if len(state.Entries) != len(want) {
		t.Fatalf("entries = %d, want %d: %+v", len(state.Entries), len(want), state.Entries)
	}
	for i, entry := range state.Entries {
		if entry.Source != want[i].source || entry.Destination != want[i].destination ||
			entry.Interface != want[i].iface || entry.Family != want[i].family {
			t.Fatalf("entry %d = %+v, want source=%s dest=%s iface=%s family=%s",
				i, entry, want[i].source, want[i].destination, want[i].iface, want[i].family)
		}
		if strings.Contains(strings.Join(entry.Args, " "), "(v6)") {
			t.Fatalf("parsed (v6) into restore args: %+v", entry)
		}
	}
	restricted := state.Entries[0]
	if !containsUFWArg(restricted.Args, "from", "10.0.0.0/8") {
		t.Fatalf("restricted rule args %v missing from 10.0.0.0/8", restricted.Args)
	}
	if len(restricted.Args) >= 2 && restricted.Args[0] == "allow" && restricted.Args[1] == "22/tcp" {
		t.Fatalf("restricted rule used unrestricted simple syntax: %v", restricted.Args)
	}
}

func TestRestoreUFWStateReplaysSourceInterfaceAndIPv6Constraints(t *testing.T) {
	model := &transactionalUFWModel{enabled: true, rules: map[string]string{}}
	initial, err := parseUFWStatus(strings.Join([]string{
		"Status: active",
		"To                         Action      From",
		"--                         ------      ----",
		"22/tcp                     ALLOW       10.0.0.0/8",
		"22/tcp                     ALLOW       Anywhere",
		"22/tcp on eth0             ALLOW       Anywhere",
		"22/tcp (v6)                ALLOW       2001:db8::/32",
		"22/tcp (v6)                ALLOW       Anywhere (v6)",
		"443/tcp                    ALLOW       Anywhere",
		"443/tcp (v6)               ALLOW       Anywhere (v6)",
	}, "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := restoreUFWState(context.Background(), model.runner, initial, nil); err != nil {
		t.Fatal(err)
	}
	assertUFWRestoreCall(t, model.calls, []string{"allow", "from", "10.0.0.0/8", "to", "any", "port", "22", "proto", "tcp"})
	assertUFWRestoreCall(t, model.calls, []string{"allow", "22/tcp"})
	assertUFWRestoreCall(t, model.calls, []string{"allow", "in", "on", "eth0", "to", "any", "port", "22", "proto", "tcp"})
	assertUFWRestoreCall(t, model.calls, []string{"allow", "from", "2001:db8::/32", "to", "any", "port", "22", "proto", "tcp"})
	assertUFWRestoreCall(t, model.calls, []string{"allow", "443/tcp"})
	for _, call := range model.calls {
		joined := strings.Join(call, " ")
		if strings.Contains(joined, "(v6)") {
			t.Fatalf("restore replayed IPv6 marker as ufw args: %v", call)
		}
	}
}

func TestRestoreUFWStateRebuildsWidenedJournalArgsFromSource(t *testing.T) {
	model := &transactionalUFWModel{enabled: true, rules: map[string]string{}}
	initial := ufwState{
		Enabled: true,
		Entries: []ufwRuleState{{
			Family:      "ipv4",
			Action:      "allow",
			Direction:   "in",
			Source:      "10.0.0.0/8",
			Destination: "22/tcp",
			Protocol:    "tcp",
			Args:        []string{"allow", "22/tcp"},
		}},
	}
	if err := restoreUFWState(context.Background(), model.runner, initial, nil); err != nil {
		t.Fatal(err)
	}
	assertUFWRestoreCall(t, model.calls, []string{"allow", "from", "10.0.0.0/8", "to", "any", "port", "22", "proto", "tcp"})
	for _, call := range model.calls {
		args := ufwArgsFromCall(call)
		if len(args) >= 2 && args[0] == "allow" && args[1] == "22/tcp" {
			t.Fatalf("leftover journal restore widened to allow-from-anywhere: %v", call)
		}
	}
}

func ufwArgsFromCall(call []string) []string {
	for i, part := range call {
		if part == "ufw" && i+1 < len(call) {
			return call[i+1:]
		}
	}
	return call
}

func containsUFWArg(args []string, key, value string) bool {
	for i, arg := range args {
		if arg == key && i+1 < len(args) && args[i+1] == value {
			return true
		}
	}
	return false
}

func assertUFWRestoreCall(t *testing.T, calls [][]string, want []string) {
	t.Helper()
	for _, call := range calls {
		args := ufwArgsFromCall(call)
		if reflect.DeepEqual(args, want) {
			return
		}
	}
	t.Fatalf("missing restore call %v in %v", want, calls)
}

func TestReconcileKeepsInstallTimeSSHAndACMEComments(t *testing.T) {
	model := &transactionalUFWModel{enabled: true, rules: map[string]string{
		"22/tcp":   "Veil management SSH",
		"80/tcp":   "Veil ACME HTTP-01",
		"9999/tcp": "Veil stale",
	}}
	if _, err := runFirewallRules(context.Background(), model.runner, transactionalFirewallRequest()); err != nil {
		t.Fatal(err)
	}
	if _, exists := model.rules["22/tcp"]; !exists {
		t.Fatalf("management SSH was deleted: %v", model.rules)
	}
	if _, exists := model.rules["80/tcp"]; !exists {
		t.Fatalf("ACME HTTP-01 was deleted: %v", model.rules)
	}
	if _, exists := model.rules["9999/tcp"]; exists {
		t.Fatalf("stale Veil rule remains: %v", model.rules)
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
