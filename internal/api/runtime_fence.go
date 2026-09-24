package api

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	veilapply "github.com/mikkelchokolate/Veil/internal/apply"
	"github.com/mikkelchokolate/Veil/internal/privileged"
	"github.com/mikkelchokolate/Veil/internal/storage"
)

// runtimeFenceLeaseTTL bounds panel-minted fencing leases for one-shot
// privileged mutations (key rotation, backup create/delete/prune). It matches
// the generous bound the restore path already uses: a crashed holder's lease
// is still expired early by the apply runner's dead-owner sweep, or by the
// dead-owner retry in acquireRuntimeFence.
const runtimeFenceLeaseTTL = 2 * time.Hour

// acquireRuntimeFence claims the durable apply lease for a one-shot privileged
// mutation and returns the fencing token the helper expects when RequireFence
// is configured. The returned release function must be invoked when the
// mutation finishes so other operations are not blocked.
//
// When no lease store is available (no durable database, e.g. in-process
// dev/test adapters), an empty token with a no-op release is returned. Local
// adapters configured with RequireFence=false accept it, while a production
// helper rejects it — callers decide whether that rejection is fatal (direct
// mutations) or tolerable (a no-op startup recovery probe).
//
// Callers serialize the s.db read themselves: key rotation holds s.mu and
// backup mutations hold backupMutationMu (restore replaces s.db while holding
// the same mutex), so the field is stable in every caller.
func (s *managementState) acquireRuntimeFence(operation string) (privileged.FenceToken, func(), error) {
	store, cleanup, err := s.fencingLeaseStore()
	if err != nil {
		return privileged.FenceToken{}, nil, err
	}
	if store == nil {
		return privileged.FenceToken{}, func() {}, nil
	}
	now := time.Now().UTC()
	owner := fmt.Sprintf("pid:%d:%s", os.Getpid(), uuid.NewString())
	lease, acquired, err := store.Acquire(owner, operation, now, runtimeFenceLeaseTTL)
	if err == nil && !acquired {
		// A crashed panel can leave a live-looking lease behind. The apply
		// runner normally expires dead owners, but startup recovery runs before
		// the runner starts — expire a dead owner here too and retry once so
		// recovery is not wedged behind a stale lease.
		if current, curErr := store.Current(); curErr == nil && current.Owner != "" && !veilapply.ProcessOwnerAlive(current.Owner) {
			if expireErr := store.Expire(current.Owner, current.Generation); expireErr == nil || errors.Is(expireErr, veilapply.ErrApplyLeaseLost) {
				lease, acquired, err = store.Acquire(owner, operation, now, runtimeFenceLeaseTTL)
			}
		}
	}
	if err != nil {
		cleanup()
		return privileged.FenceToken{}, nil, err
	}
	if !acquired {
		cleanup()
		return privileged.FenceToken{}, nil, errors.New("fencing lease is held by another operation")
	}
	release := func() {
		defer cleanup()
		if releaseErr := store.Release(lease.Owner, lease.Generation); releaseErr != nil {
			log.Printf("release %s fencing lease: %v", operation, releaseErr)
		}
	}
	return privileged.FenceToken{
		Owner:          lease.Owner,
		Generation:     lease.Generation,
		LeaseExpiresAt: lease.ExpiresAt,
		OperationID:    lease.Operation,
	}, release, nil
}

// fencingLeaseStore resolves the durable apply lease store. When the panel
// database is open it is reused; otherwise the on-disk veil.db is opened just
// long enough to mint the token — startup key-rotation recovery runs before
// the apply subsystem opens it. A nil store means no lease can be minted and
// the caller proceeds unfenced (only unfenced local adapters will accept it).
func (s *managementState) fencingLeaseStore() (*veilapply.LeaseStore, func(), error) {
	if s.db != nil {
		return veilapply.NewLeaseStore(s.db), func() {}, nil
	}
	if s.statePath == "" {
		return nil, func() {}, nil
	}
	databasePath := filepath.Join(filepath.Dir(s.statePath), "veil.db")
	if _, err := os.Stat(databasePath); err != nil {
		return nil, func() {}, nil
	}
	opener := s.databaseOpener
	if opener == nil {
		opener = storage.Open
	}
	db, err := opener(databasePath)
	if err != nil {
		return nil, nil, fmt.Errorf("open fencing lease store: %w", err)
	}
	return veilapply.NewLeaseStore(db), func() { _ = db.Close() }, nil
}

// privilegedFenceRejected reports whether the helper rejected the operation
// for a missing, expired, or stale fencing token.
func privilegedFenceRejected(err error) bool {
	var operationError *privileged.Error
	return errors.As(err, &operationError) && operationError.Code == privileged.ErrorConflict
}
