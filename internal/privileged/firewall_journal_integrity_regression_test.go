package privileged

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Regression for #1227: the firewall transaction journal lives under a root
// inside the service-writable state tree, so an attacker can swap the
// directory and plant a journal that recovery would replay as root. The
// helper now pins the root on a descriptor and refuses it unless it is the
// helper-owned 0700 directory it created — before any journal byte is read.
func TestFirewallJournalRootRejectsWrongMode(t *testing.T) {
	root := filepath.Join(t.TempDir(), "transactions")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := openFirewallJournalRoot(root); err == nil {
		t.Fatal("transaction root with group/other permissions was accepted")
	}
}

func TestFirewallJournalRootRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.MkdirAll(real, 0o700); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(root, "linked")
	if err := os.Symlink(real, linked); err != nil {
		t.Fatal(err)
	}
	if _, err := openFirewallJournalRoot(linked); err == nil {
		t.Fatal("symlinked transaction root was accepted")
	}
}

func TestFirewallRecoveryFailsClosedOnUnverifiedJournalRoot(t *testing.T) {
	// The planted journal below is never read: the swapped root fails the
	// ownership/mode check before recovery touches journal contents.
	root := filepath.Join(t.TempDir(), "transactions")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	writeStickyFirewallJournal(t, root, "prepared")
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	var ranUFW bool
	config := ProductionConfig{
		PromotionBackupRoot: root,
		RunCommand: func(context.Context, []string, time.Duration) (string, error) {
			ranUFW = true
			return "", errors.New("unexpected ufw invocation")
		},
	}
	if err := recoverFirewallTransaction(context.Background(), config); err == nil {
		t.Fatal("recovery consumed a journal from an unverified root")
	}
	if ranUFW {
		t.Fatal("planted journal drove a ufw invocation")
	}
	if _, err := os.Stat(firewallJournalPath(root)); err != nil {
		t.Fatalf("journal missing after rejected recovery: %v", err)
	}
}

func TestFirewallJournalRejectsUnknownPhase(t *testing.T) {
	root := t.TempDir()
	journal := firewallTransactionJournal{
		Version: firewallJournalVersion, TransactionID: "planted", Phase: "bogus",
	}
	body, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(firewallJournalPath(root), body, 0o600); err != nil {
		t.Fatal(err)
	}
	dir, err := openFirewallJournalRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	if _, err := readFirewallJournal(dir); err == nil {
		t.Fatal("journal with an unknown phase was accepted")
	}
}

func TestFirewallJournalRejectsMalformedInitialEntries(t *testing.T) {
	root := t.TempDir()
	journal := firewallTransactionJournal{
		Version: firewallJournalVersion, TransactionID: "planted", Phase: "prepared",
		Initial: ufwState{
			Enabled: true,
			Entries: []ufwRuleState{{
				Family: "ipv4", Action: "frobnicate", Direction: "in", Destination: "22/tcp",
			}},
		},
	}
	body, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(firewallJournalPath(root), body, 0o600); err != nil {
		t.Fatal(err)
	}
	var ranUFW bool
	config := ProductionConfig{
		PromotionBackupRoot: root,
		RunCommand: func(context.Context, []string, time.Duration) (string, error) {
			ranUFW = true
			return "", errors.New("unexpected ufw invocation")
		},
	}
	// Recovery fails closed on the malformed entry and quarantines the journal
	// instead of replaying it.
	if err := recoverFirewallTransaction(context.Background(), config); err == nil {
		t.Fatal("recovery replayed a journal with a malformed initial entry")
	}
	if ranUFW {
		t.Fatal("malformed journal drove a ufw invocation")
	}
	if _, err := os.Stat(firewallJournalPath(root) + ".quarantined"); err != nil {
		t.Fatalf("malformed journal was not quarantined: %v", err)
	}
}
