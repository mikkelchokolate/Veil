//go:build unix

package api

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/client"
	"golang.org/x/sys/unix"
)

// TestExpirationReserveHonorsSnapshotBarrier covers #987: the desired-revision
// + immutable-snapshot reservation must serialize against the cross-process
// snapshot barrier so a helper-run backup cannot capture state.json and
// veil.db mid-commit.
func TestExpirationReserveHonorsSnapshotBarrier(t *testing.T) {
	_, state := newApplyTrackedRouterWithState(t)
	state.expirationReconciler.Stop()
	expired := time.Now().UTC().Add(-time.Minute).Unix()
	row, err := state.clientRepo.Create(client.Client{
		Name: "barrier-expiry", Enabled: true, QuotaResetPolicy: client.ResetNever, ExpiresAt: &expired,
	})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("client=%s;generation=%d;expires=%d;depleted=true", row.ID, row.Version, expired)))
	candidate := expiredClientCandidate{
		ID: row.ID, ExpiresAt: expired, CreatedAt: row.CreatedAt,
		TargetGeneration: int64(row.Version), TargetPayloadHash: hex.EncodeToString(digest[:]),
	}

	// Hold the snapshot barrier the same way a privileged backup capture does.
	barrierPath := filepath.Join(filepath.Dir(state.statePath), ".veil-snapshot.lock")
	barrierFile, err := os.OpenFile(barrierPath, os.O_CREATE|os.O_RDWR, 0o666)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Flock(int(barrierFile.Fd()), unix.LOCK_EX); err != nil {
		t.Fatalf("lock snapshot barrier: %v", err)
	}

	type reserveResult struct {
		revision uint64
		done     bool
		err      error
	}
	resultCh := make(chan reserveResult, 1)
	go func() {
		state.mu.Lock()
		revision, done, err := state.reserveExpirationRevisionLocked(candidate, time.Now().UTC().Unix())
		state.mu.Unlock()
		resultCh <- reserveResult{revision: revision, done: done, err: err}
	}()

	select {
	case res := <-resultCh:
		t.Fatalf("revision reservation committed while the snapshot barrier was held: %+v", res)
	case <-time.After(500 * time.Millisecond):
	}

	if err := unix.Flock(int(barrierFile.Fd()), unix.LOCK_UN); err != nil {
		t.Fatalf("unlock snapshot barrier: %v", err)
	}
	_ = barrierFile.Close()

	select {
	case res := <-resultCh:
		if res.err != nil || res.done || res.revision == 0 {
			t.Fatalf("reservation after barrier release: %+v", res)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("revision reservation did not complete after the barrier was released")
	}
}
