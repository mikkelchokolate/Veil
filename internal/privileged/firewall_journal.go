package privileged

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/mikkelchokolate/Veil/internal/safefs"
	"golang.org/x/sys/unix"
)

const (
	firewallJournalVersion = 1
	firewallJournalName    = ".firewall-transaction.json"
	firewallLockName       = ".firewall.lock"
)

type firewallTransactionJournal struct {
	Version       int              `json:"version"`
	TransactionID string           `json:"transactionId"`
	Phase         string           `json:"phase"`
	Initial       ufwState         `json:"initial"`
	Desired       ResolvedFirewall `json:"desired"`
	UpdatedAt     int64            `json:"updatedAt"`
	Fence         FenceToken       `json:"fence"`
}

func sameFirewallFence(left, right FenceToken) bool {
	return left.Owner == right.Owner && left.Generation == right.Generation && left.OperationID == right.OperationID
}

func firewallJournalPath(root string) string {
	return filepath.Join(root, firewallJournalName)
}

// openFirewallJournalRoot pins the transaction root on a directory descriptor
// and proves it is the helper-owned 0700 directory the helper itself created.
// The root lives under the service-writable state tree, so an attacker can
// swap or symlink it; a leaf that is not an owned 0700 directory fails closed
// before any journal inside it is trusted (#1227).
func openFirewallJournalRoot(root string) (*safefs.Dir, error) {
	dir, err := safefs.OpenDir(root)
	if err != nil {
		return nil, err
	}
	info, err := dir.File().Stat()
	if err != nil {
		_ = dir.Close()
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(effectiveUID()) || info.Mode().Perm() != 0o700 {
		_ = dir.Close()
		return nil, fmt.Errorf("firewall transaction root %s is not a helper-owned 0700 directory (uid=%d mode=%#o)", root, stat.Uid, info.Mode().Perm())
	}
	return dir, nil
}

func withFirewallLock(root string, action func(*safefs.Dir) (FirewallResult, error)) (result FirewallResult, resultErr error) {
	if root == "" {
		return FirewallResult{}, errors.New("firewall transaction root is required")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return FirewallResult{}, err
	}
	dir, err := openFirewallJournalRoot(root)
	if err != nil {
		return FirewallResult{}, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, dir.Close())
	}()
	lock, err := openLockFileAt(dir, firewallLockName)
	if err != nil {
		return FirewallResult{}, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, releaseLockedFile(lock))
	}()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX); err != nil {
		return FirewallResult{}, err
	}
	return action(dir)
}

func runFirewallTransaction(ctx context.Context, config ProductionConfig, request ResolvedFirewall) (FirewallResult, error) {
	return withFirewallLock(config.PromotionBackupRoot, func(dir *safefs.Dir) (FirewallResult, error) {
		action := request.Action
		if action == "" {
			action = FirewallActionApply
		}
		switch action {
		case FirewallActionCommit:
			journal, err := readFirewallJournal(dir)
			if err != nil {
				return FirewallResult{}, err
			}
			if journal.TransactionID != request.TransactionID || journal.Phase != "applied" {
				return FirewallResult{}, errors.New("firewall transaction is not prepared for commit")
			}
			if !sameFirewallFence(journal.Fence, request.Fence) {
				return FirewallResult{}, errors.New("firewall transaction fence mismatch")
			}
			if err := removeFirewallJournal(dir); err != nil {
				return FirewallResult{}, err
			}
			return FirewallResult{TransactionID: request.TransactionID}, nil
		case FirewallActionRollback:
			journal, err := readFirewallJournal(dir)
			if err != nil {
				return FirewallResult{}, err
			}
			if journal.TransactionID != request.TransactionID {
				return FirewallResult{}, errors.New("firewall transaction ID mismatch")
			}
			if !sameFirewallFence(journal.Fence, request.Fence) {
				return FirewallResult{}, errors.New("firewall transaction fence mismatch")
			}
			if err := rollbackFirewallJournal(ctx, config.RunCommand, journal); err != nil {
				return FirewallResult{}, err
			}
			if err := removeFirewallJournal(dir); err != nil {
				return FirewallResult{}, err
			}
			return FirewallResult{TransactionID: request.TransactionID}, nil
		case FirewallActionApply, FirewallActionPrepare:
		default:
			return FirewallResult{}, errors.New("unsupported firewall transaction action")
		}

		if err := recoverFirewallTransactionLocked(ctx, config, dir); err != nil {
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
			Initial: initial, Desired: request, UpdatedAt: time.Now().UTC().Unix(), Fence: request.Fence,
		}
		if err := writeFirewallJournal(dir, journal); err != nil {
			return FirewallResult{}, err
		}
		result, err := reconcileUFW(ctx, config.RunCommand, request)
		if err != nil {
			watchdogCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
			rollbackErr := rollbackFirewallJournal(watchdogCtx, config.RunCommand, journal)
			cancel()
			if rollbackErr != nil {
				journal.Phase = "rollback-failed"
				journal.UpdatedAt = time.Now().UTC().Unix()
				writeErr := writeFirewallJournal(dir, journal)
				return FirewallResult{}, errors.Join(err, fmt.Errorf("firewall rollback failed: %w", rollbackErr), writeErr)
			}
			if removeErr := removeFirewallJournal(dir); removeErr != nil {
				return FirewallResult{}, errors.Join(err, removeErr)
			}
			return FirewallResult{}, err
		}
		journal.Phase = "applied"
		journal.UpdatedAt = time.Now().UTC().Unix()
		if err := writeFirewallJournal(dir, journal); err != nil {
			return FirewallResult{}, err
		}
		result.TransactionID = transactionID
		if action == FirewallActionPrepare {
			result.Prepared = true
			return result, nil
		}
		if err := removeFirewallJournal(dir); err != nil {
			return FirewallResult{}, err
		}
		return result, nil
	})
}

func recoverFirewallTransaction(ctx context.Context, config ProductionConfig) error {
	_, err := withFirewallLock(config.PromotionBackupRoot, func(dir *safefs.Dir) (FirewallResult, error) {
		return FirewallResult{}, recoverFirewallTransactionLocked(ctx, config, dir)
	})
	return err
}

func recoverFirewallTransactionLocked(ctx context.Context, config ProductionConfig, dir *safefs.Dir) error {
	journal, err := readFirewallJournal(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if journal.Phase == "applied" {
		return removeFirewallJournal(dir)
	}
	if err := rollbackFirewallJournal(ctx, config.RunCommand, journal); err != nil {
		if qerr := quarantineFirewallJournal(dir); qerr != nil {
			return errors.Join(err, qerr)
		}
		return err
	}
	return removeFirewallJournal(dir)
}

func quarantineFirewallJournal(dir *safefs.Dir) error {
	if err := dir.RenameAt(firewallJournalName, firewallJournalName+".quarantined"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return dir.File().Sync()
}

func rollbackFirewallJournal(_ context.Context, runner CommandRunner, journal firewallTransactionJournal) error {
	desired, err := parseDesiredUFWRules(journal.Desired)
	if err != nil {
		return fmt.Errorf("parse journal desired rules: %w", err)
	}
	// A journal can only come from the verified helper-owned root, but the
	// replay set is still validated before any ufw mutation runs: recovered
	// entries must parse into the managed rule grammar rather than executing
	// whatever a persisted record contains (#1227).
	for _, entry := range journal.Initial.Entries {
		if _, _, err := ufwReplayArgs(entry); err != nil {
			return fmt.Errorf("firewall journal initial state is invalid: %w", err)
		}
	}
	watchdogCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	return restoreUFWState(watchdogCtx, runner, journal.Initial, desired)
}

func writeFirewallJournal(dir *safefs.Dir, journal firewallTransactionJournal) error {
	payload, err := json.Marshal(journal)
	if err != nil {
		return err
	}
	return atomicWriteFileAt(dir, firewallJournalName, payload, 0o600)
}

// atomicWriteFileAt stages the journal via a create-exclusive temp sibling and
// renames it inside the pinned directory, so publication can never be
// redirected by a swapped ancestor or leaf.
func atomicWriteFileAt(dir *safefs.Dir, name string, body []byte, mode os.FileMode) (err error) {
	tmp, tmpName, err := dir.CreateTempAt(name+".tmp-", mode)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		_ = tmp.Close()
		if !committed {
			_ = dir.RemoveAt(tmpName)
		}
	}()
	if _, err := tmp.Write(body); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := dir.RenameAt(tmpName, name); err != nil {
		return err
	}
	committed = true
	return dir.File().Sync()
}

func readFirewallJournal(dir *safefs.Dir) (firewallTransactionJournal, error) {
	var journal firewallTransactionJournal
	file, err := dir.OpenFileAt(firewallJournalName)
	if err != nil {
		return journal, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return journal, err
	}
	if !info.Mode().IsRegular() {
		return journal, errors.New("firewall transaction journal is not a regular file")
	}
	payload, err := io.ReadAll(file)
	if err != nil {
		return journal, err
	}
	if err := json.Unmarshal(payload, &journal); err != nil {
		return journal, err
	}
	if journal.Version != firewallJournalVersion || journal.TransactionID == "" {
		return journal, errors.New("invalid firewall transaction journal")
	}
	switch journal.Phase {
	case "prepared", "applied", "rollback-failed":
	default:
		return journal, fmt.Errorf("firewall transaction journal phase %q is invalid", journal.Phase)
	}
	return journal, nil
}

func removeFirewallJournal(dir *safefs.Dir) error {
	if err := dir.RemoveAt(firewallJournalName); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return dir.File().Sync()
}
