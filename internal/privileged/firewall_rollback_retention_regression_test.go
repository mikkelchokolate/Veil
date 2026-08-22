package privileged

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFirewallRollbackFailureRetainsJournalForStartupRecovery(t *testing.T) {
	root := t.TempDir()
	request := transactionalFirewallRequest()
	request.Action = FirewallActionPrepare
	failing := func(_ context.Context, command []string, _ time.Duration) (string, error) {
		for _, part := range command {
			if part == "status" {
				return "Status: inactive\n22/tcp ALLOW Anywhere # OpenSSH\n", nil
			}
		}
		return "injected mutation failure", errors.New("injected mutation and rollback failure")
	}
	config := ProductionConfig{PromotionBackupRoot: root, RunCommand: failing}
	if _, err := runFirewallTransaction(context.Background(), config, request); err == nil {
		t.Fatal("expected firewall reconciliation failure")
	}
	journalPath := filepath.Join(root, ".firewall-transaction.json")
	journal, err := readFirewallJournal(root)
	if err != nil {
		t.Fatalf("rollback failure deleted recovery journal: %v", err)
	}
	if journal.Phase != "rollback-failed" {
		t.Fatalf("journal phase=%q, want rollback-failed", journal.Phase)
	}

	model := &transactionalUFWModel{rules: map[string]string{"22/tcp": "OpenSSH"}}
	config.RunCommand = model.runner
	if err := recoverFirewallTransaction(context.Background(), config); err != nil {
		t.Fatalf("startup rollback recovery: %v", err)
	}
	if _, err := os.Stat(journalPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("recovered firewall journal remains: %v", err)
	}
}
