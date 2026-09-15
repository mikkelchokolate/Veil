package privileged

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRecoverKeyRotationIgnoresStickyAppliedFirewallJournal(t *testing.T) {
	root := t.TempDir()
	writeStickyFirewallJournal(t, root, "applied")
	var ranUFW bool
	executor := NewProductionExecutor(ProductionConfig{
		PromotionBackupRoot: root,
		StatePath:           filepath.Join(root, "state.json"),
		KeyPath:             filepath.Join(root, "state.key"),
		RunCommand: func(context.Context, []string, time.Duration) (string, error) {
			ranUFW = true
			return "injected ufw restore failure", errors.New("injected ufw restore failure")
		},
	})
	if err := executor.RecoverKeyRotation(context.Background()); err != nil {
		t.Fatalf("RecoverKeyRotation: %v", err)
	}
	if ranUFW {
		t.Fatal("applied firewall journal must be committed without UFW rollback")
	}
	if _, err := os.Stat(firewallJournalPath(root)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("applied firewall journal remains: %v", err)
	}
}

func TestRecoverKeyRotationQuarantinesUnrollbackablePreparedJournal(t *testing.T) {
	root := t.TempDir()
	writeStickyFirewallJournal(t, root, "prepared")
	executor := NewProductionExecutor(ProductionConfig{
		PromotionBackupRoot: root,
		StatePath:           filepath.Join(root, "state.json"),
		KeyPath:             filepath.Join(root, "state.key"),
		RunCommand: func(context.Context, []string, time.Duration) (string, error) {
			return "ERROR: Invalid syntax", errors.New("injected ufw restore failure")
		},
	})
	if err := executor.RecoverKeyRotation(context.Background()); err != nil {
		t.Fatalf("RecoverKeyRotation: %v", err)
	}
	if _, err := os.Stat(firewallJournalPath(root)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failing prepared journal was not quarantined: %v", err)
	}
	if _, err := os.Stat(firewallJournalPath(root) + ".quarantined"); err != nil {
		t.Fatalf("quarantined firewall journal missing: %v", err)
	}
}

func TestFirewallAppliedJournalIsCommittedNotRolledBack(t *testing.T) {
	root := t.TempDir()
	writeStickyFirewallJournal(t, root, "applied")
	var commands []string
	config := ProductionConfig{
		PromotionBackupRoot: root,
		RunCommand: func(_ context.Context, command []string, _ time.Duration) (string, error) {
			commands = append(commands, strings.Join(command, " "))
			return "Status: inactive\n", nil
		},
	}
	if err := recoverFirewallTransaction(context.Background(), config); err != nil {
		t.Fatalf("recover applied journal: %v", err)
	}
	for _, command := range commands {
		if strings.Contains(command, "delete") || strings.Contains(command, "allow") {
			t.Fatalf("applied journal triggered UFW rollback: %q", command)
		}
	}
	if _, err := os.Stat(firewallJournalPath(root)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("applied journal remains: %v", err)
	}
}

func writeStickyFirewallJournal(t *testing.T, root, phase string) {
	t.Helper()
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	journal := firewallTransactionJournal{
		Version:       firewallJournalVersion,
		TransactionID: "sticky-firewall",
		Phase:         phase,
		Initial:       ufwState{Enabled: false, Rules: map[string]string{"22/tcp": "OpenSSH"}},
		Desired:       transactionalFirewallRequest(),
		UpdatedAt:     time.Now().Unix(),
	}
	body, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(firewallJournalPath(root), body, 0o600); err != nil {
		t.Fatal(err)
	}
}
