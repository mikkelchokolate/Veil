package privileged

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

type firewallCrashState struct {
	Enabled bool              `json:"enabled"`
	Rules   map[string]string `json:"rules"`
}

func TestFirewallTransactionRecoversExactStateAfterSIGKILL(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "ufw-state.json")
	signalPath := filepath.Join(root, "rule-staged")
	transactionRoot := filepath.Join(root, "transactions")
	if err := os.MkdirAll(transactionRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	initial := firewallCrashState{Enabled: false, Rules: map[string]string{
		"22/tcp": "OpenSSH", "9999/tcp": "Veil old runtime",
	}}
	writeFirewallCrashState(t, statePath, initial)

	command := exec.Command(os.Args[0], "-test.run=^TestFirewallTransactionSubprocessHelper$")
	command.Env = append(os.Environ(),
		"VEIL_FIREWALL_HELPER=apply",
		"VEIL_FIREWALL_STATE="+statePath,
		"VEIL_FIREWALL_SIGNAL="+signalPath,
		"VEIL_FIREWALL_TRANSACTION_ROOT="+transactionRoot,
	)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(signalPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			_ = command.Process.Kill()
			_ = command.Wait()
			t.Fatal("firewall subprocess did not stage a rule")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = command.Wait()
	modified := readFirewallCrashState(t, statePath)
	if _, ok := modified.Rules["2096/tcp"]; !ok {
		t.Fatalf("fault injection did not mutate UFW state: %+v", modified)
	}
	journalPath := filepath.Join(transactionRoot, ".firewall-transaction.json")
	if _, err := os.Stat(journalPath); err != nil {
		t.Errorf("durable firewall transaction journal missing after SIGKILL: %v", err)
	}

	recoverCommand := exec.Command(os.Args[0], "-test.run=^TestFirewallTransactionSubprocessHelper$")
	recoverCommand.Env = append(os.Environ(),
		"VEIL_FIREWALL_HELPER=recover",
		"VEIL_FIREWALL_STATE="+statePath,
		"VEIL_FIREWALL_TRANSACTION_ROOT="+transactionRoot,
	)
	if output, err := recoverCommand.CombinedOutput(); err != nil {
		t.Fatalf("fresh helper recovery: %v\n%s", err, output)
	}
	recovered := readFirewallCrashState(t, statePath)
	if !equalFirewallCrashState(initial, recovered) {
		t.Fatalf("firewall recovery mismatch: initial=%+v recovered=%+v", initial, recovered)
	}
	if _, err := os.Stat(journalPath); !os.IsNotExist(err) {
		t.Fatalf("firewall transaction journal remains after recovery: %v", err)
	}
}

func TestFirewallTransactionSubprocessHelper(t *testing.T) {
	mode := os.Getenv("VEIL_FIREWALL_HELPER")
	if mode == "" {
		t.Skip("subprocess helper")
	}
	runner := firewallCrashRunner(t, os.Getenv("VEIL_FIREWALL_STATE"), os.Getenv("VEIL_FIREWALL_SIGNAL"), mode == "apply")
	executor := NewProductionExecutor(ProductionConfig{
		PromotionBackupRoot: os.Getenv("VEIL_FIREWALL_TRANSACTION_ROOT"),
		StatePath:           filepath.Join(filepath.Dir(os.Getenv("VEIL_FIREWALL_STATE")), "state.json"),
		KeyPath:             filepath.Join(filepath.Dir(os.Getenv("VEIL_FIREWALL_STATE")), "state.key"),
		RunCommand:          runner,
	})
	if mode == "recover" {
		if err := executor.RecoverKeyRotation(context.Background()); err != nil {
			t.Fatal(err)
		}
		return
	}
	request := ResolvedFirewall{
		RuleIDs: []string{"management-panel"},
		Rules:   []FirewallRule{{Command: "ufw", Args: []string{"allow", "2096/tcp", "comment", "Veil Panel"}}},
	}
	if _, err := executor.Firewall(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	t.Fatal("firewall apply unexpectedly completed instead of waiting for SIGKILL")
}

func firewallCrashRunner(t *testing.T, statePath, signalPath string, blockAfterStage bool) CommandRunner {
	t.Helper()
	return func(ctx context.Context, command []string, _ time.Duration) (string, error) {
		ufw := -1
		for index, part := range command {
			if part == "ufw" {
				ufw = index
				break
			}
		}
		if ufw < 0 || ufw+1 >= len(command) {
			return "", fmt.Errorf("unexpected command: %v", command)
		}
		args := command[ufw+1:]
		state := readFirewallCrashState(t, statePath)
		switch {
		case len(args) == 1 && args[0] == "status":
			return firewallCrashStatus(state), nil
		case args[0] == "--dry-run":
			return "Rules updated", nil
		case args[0] == "allow" && len(args) >= 2:
			comment := ""
			for i := 2; i+1 < len(args); i++ {
				if args[i] == "comment" {
					comment = args[i+1]
				}
			}
			state.Rules[args[1]] = comment
			writeFirewallCrashState(t, statePath, state)
			if blockAfterStage && args[1] == "2096/tcp" {
				if err := os.WriteFile(signalPath, []byte("staged"), 0o600); err != nil {
					t.Fatal(err)
				}
				<-ctx.Done()
				return "", ctx.Err()
			}
			return "Rule added", nil
		case len(args) >= 3 && args[0] == "delete" && args[1] == "allow":
			delete(state.Rules, args[2])
			writeFirewallCrashState(t, statePath, state)
			return "Rule deleted", nil
		case len(args) == 2 && args[0] == "--force" && args[1] == "enable":
			state.Enabled = true
			writeFirewallCrashState(t, statePath, state)
			return "enabled", nil
		case len(args) == 2 && args[0] == "--force" && args[1] == "disable":
			state.Enabled = false
			writeFirewallCrashState(t, statePath, state)
			return "disabled", nil
		case len(args) == 1 && args[0] == "reload":
			return "reloaded", nil
		default:
			return "", fmt.Errorf("unexpected ufw args: %v", args)
		}
	}
}

func firewallCrashStatus(state firewallCrashState) string {
	status := "inactive"
	if state.Enabled {
		status = "active"
	}
	lines := []string{"Status: " + status, "To Action From", "-- ------ ----"}
	targets := make([]string, 0, len(state.Rules))
	for target := range state.Rules {
		targets = append(targets, target)
	}
	sort.Strings(targets)
	for _, target := range targets {
		line := target + " ALLOW Anywhere"
		if state.Rules[target] != "" {
			line += " # " + state.Rules[target]
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func writeFirewallCrashState(t *testing.T, path string, state firewallCrashState) {
	t.Helper()
	body, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFirewallCrashState(t *testing.T, path string) firewallCrashState {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var state firewallCrashState
	if err := json.Unmarshal(body, &state); err != nil {
		t.Fatal(err)
	}
	return state
}

func equalFirewallCrashState(left, right firewallCrashState) bool {
	if left.Enabled != right.Enabled || len(left.Rules) != len(right.Rules) {
		return false
	}
	for key, value := range left.Rules {
		if right.Rules[key] != value {
			return false
		}
	}
	return true
}
