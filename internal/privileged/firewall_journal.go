package privileged

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/mikkelchokolate/Veil/internal/atomicfile"
	"golang.org/x/sys/unix"
)

const firewallJournalVersion = 1

type firewallTransactionJournal struct {
	Version       int              `json:"version"`
	TransactionID string           `json:"transactionId"`
	Phase         string           `json:"phase"`
	Initial       ufwState         `json:"initial"`
	Desired       ResolvedFirewall `json:"desired"`
	UpdatedAt     int64            `json:"updatedAt"`
}

func firewallJournalPath(root string) string {
	return filepath.Join(root, ".firewall-transaction.json")
}

func withFirewallLock(root string, action func() (FirewallResult, error)) (FirewallResult, error) {
	if root == "" {
		return FirewallResult{}, errors.New("firewall transaction root is required")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return FirewallResult{}, err
	}
	lock, err := os.OpenFile(filepath.Join(root, ".firewall.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return FirewallResult{}, err
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX); err != nil {
		return FirewallResult{}, err
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN) //nolint:errcheck
	return action()
}

func runFirewallTransaction(ctx context.Context, config ProductionConfig, request ResolvedFirewall) (FirewallResult, error) {
	return withFirewallLock(config.PromotionBackupRoot, func() (FirewallResult, error) {
		action := request.Action
		if action == "" {
			action = FirewallActionApply
		}
		switch action {
		case FirewallActionCommit:
			journal, err := readFirewallJournal(config.PromotionBackupRoot)
			if err != nil {
				return FirewallResult{}, err
			}
			if journal.TransactionID != request.TransactionID || journal.Phase != "applied" {
				return FirewallResult{}, errors.New("firewall transaction is not prepared for commit")
			}
			if err := removeFirewallJournal(config.PromotionBackupRoot); err != nil {
				return FirewallResult{}, err
			}
			return FirewallResult{TransactionID: request.TransactionID}, nil
		case FirewallActionRollback:
			journal, err := readFirewallJournal(config.PromotionBackupRoot)
			if err != nil {
				return FirewallResult{}, err
			}
			if journal.TransactionID != request.TransactionID {
				return FirewallResult{}, errors.New("firewall transaction ID mismatch")
			}
			if err := rollbackFirewallJournal(ctx, config.RunCommand, journal); err != nil {
				return FirewallResult{}, err
			}
			if err := removeFirewallJournal(config.PromotionBackupRoot); err != nil {
				return FirewallResult{}, err
			}
			return FirewallResult{TransactionID: request.TransactionID}, nil
		case FirewallActionApply, FirewallActionPrepare:
		default:
			return FirewallResult{}, errors.New("unsupported firewall transaction action")
		}

		if err := recoverFirewallTransactionLocked(ctx, config); err != nil {
			return FirewallResult{}, err
		}
		initialOutput, err := runUFW(ctx, config.RunCommand, 10*time.Second, "status")
		if err != nil {
			return FirewallResult{}, fmt.Errorf("preflight ufw status: %w", err)
		}
		initial, err := parseUFWStatus(initialOutput)
		if err != nil {
			return FirewallResult{}, err
		}
		transactionID := uuid.NewString()
		journal := firewallTransactionJournal{
			Version: firewallJournalVersion, TransactionID: transactionID, Phase: "prepared",
			Initial: initial, Desired: request, UpdatedAt: time.Now().UTC().Unix(),
		}
		if err := writeFirewallJournal(config.PromotionBackupRoot, journal); err != nil {
			return FirewallResult{}, err
		}
		result, err := reconcileUFW(ctx, config.RunCommand, request)
		if err != nil {
			_ = removeFirewallJournal(config.PromotionBackupRoot)
			return FirewallResult{}, err
		}
		journal.Phase = "applied"
		journal.UpdatedAt = time.Now().UTC().Unix()
		if err := writeFirewallJournal(config.PromotionBackupRoot, journal); err != nil {
			return FirewallResult{}, err
		}
		result.TransactionID = transactionID
		if action == FirewallActionPrepare {
			result.Prepared = true
			return result, nil
		}
		if err := removeFirewallJournal(config.PromotionBackupRoot); err != nil {
			return FirewallResult{}, err
		}
		return result, nil
	})
}

func recoverFirewallTransaction(ctx context.Context, config ProductionConfig) error {
	_, err := withFirewallLock(config.PromotionBackupRoot, func() (FirewallResult, error) {
		return FirewallResult{}, recoverFirewallTransactionLocked(ctx, config)
	})
	return err
}

func recoverFirewallTransactionLocked(ctx context.Context, config ProductionConfig) error {
	journal, err := readFirewallJournal(config.PromotionBackupRoot)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := rollbackFirewallJournal(ctx, config.RunCommand, journal); err != nil {
		return err
	}
	return removeFirewallJournal(config.PromotionBackupRoot)
}

func rollbackFirewallJournal(ctx context.Context, runner CommandRunner, journal firewallTransactionJournal) error {
	if journal.Version != firewallJournalVersion || journal.TransactionID == "" {
		return errors.New("invalid firewall transaction journal")
	}
	desired, err := parseDesiredUFWRules(journal.Desired)
	if err != nil {
		return fmt.Errorf("parse journal desired rules: %w", err)
	}
	return restoreUFWState(ctx, runner, journal.Initial, desired)
}

func writeFirewallJournal(root string, journal firewallTransactionJournal) error {
	payload, err := json.Marshal(journal)
	if err != nil {
		return err
	}
	return atomicfile.Write(firewallJournalPath(root), payload, 0o600, 0o700)
}

func readFirewallJournal(root string) (firewallTransactionJournal, error) {
	var journal firewallTransactionJournal
	payload, err := os.ReadFile(firewallJournalPath(root))
	if err != nil {
		return journal, err
	}
	if err := json.Unmarshal(payload, &journal); err != nil {
		return journal, err
	}
	return journal, nil
}

func removeFirewallJournal(root string) error {
	if err := os.Remove(firewallJournalPath(root)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	dir, err := os.Open(root)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
