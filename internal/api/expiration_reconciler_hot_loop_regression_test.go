package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/client"
)

// Issue #1423: a due client whose enforcement failed and is in retry backoff
// must not pin nextBoundary to its past expires_at; the boundary is the retry
// time. A stale failed row for an older target must not hide the due client.
func TestExpirationNextBoundarySkipsDueClientInRetryBackoff(t *testing.T) {
	_, state := newApplyTrackedRouterWithState(t)
	state.expirationReconciler.Stop()
	now := time.Now().UTC().Unix()
	due := now - 5
	row, err := state.clientRepo.Create(client.Client{
		Name: "due-backoff", Enabled: true, QuotaResetPolicy: client.ResetNever, ExpiresAt: &due,
	})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("client=%s;generation=%d;expires=%d;depleted=true", row.ID, row.Version, due)))
	retryAt := now + 128
	if _, err := state.db.Exec(`INSERT INTO expiration_enforcement(
		client_id,target_generation,target_payload_hash,target_expires_at,state,
		desired_revision,applied_revision,effective_at,next_retry_at,last_error,attempts,updated_at)
		VALUES(?,?,?,?,'failed',1,0,?,?,'apply failed',1,?)`,
		row.ID, row.Version, hex.EncodeToString(digest[:]), due, due, retryAt, now); err != nil {
		t.Fatal(err)
	}
	boundary, ok, err := state.expirationReconciler.nextBoundary(context.Background())
	if err != nil || !ok {
		t.Fatalf("nextBoundary: ok=%v err=%v", ok, err)
	}
	if boundary != retryAt {
		t.Fatalf("nextBoundary=%d, want the retry time %d (now=%d); a past boundary spins the loop for the whole backoff", boundary, retryAt, now)
	}

	// The failed row targets an older expiry: the client's current target is
	// unenforced and due, so the boundary must be its expires_at again.
	moved := now - 2
	if _, err := state.db.Exec(`UPDATE clients SET expires_at=? WHERE id=?`, moved, row.ID); err != nil {
		t.Fatal(err)
	}
	boundary, ok, err = state.expirationReconciler.nextBoundary(context.Background())
	if err != nil || !ok {
		t.Fatalf("nextBoundary stale failed row: ok=%v err=%v", ok, err)
	}
	if boundary != moved {
		t.Fatalf("nextBoundary=%d, want the re-targeted due expiry %d", boundary, moved)
	}
}

func TestExpirationLoopWaitFloorsPastBoundaries(t *testing.T) {
	for _, tc := range []struct {
		in, want time.Duration
	}{
		{-time.Hour, expirationMinLoopWait},
		{0, expirationMinLoopWait},
		{250 * time.Millisecond, 250 * time.Millisecond},
		{time.Minute, time.Minute},
		{time.Hour, expirationSafetySweepInterval},
	} {
		if got := expirationLoopWait(tc.in); got != tc.want {
			t.Errorf("expirationLoopWait(%v)=%v, want %v", tc.in, got, tc.want)
		}
	}
}

// Issue #1423: while enforcement is paused (runtime verification unknown,
// e.g. right after a restore) the reconciler must back off instead of
// re-listing the due set with no sleep, and must resume promptly once the
// pause clears and it is signalled.
func TestExpirationReconcilerBacksOffWhileEnforcementPaused(t *testing.T) {
	_, state := newApplyTrackedRouterWithState(t)
	state.expirationReconciler.Stop()
	due := time.Now().UTC().Unix() - 5
	var ids []string
	for i := 0; i < 3; i++ {
		row, err := state.clientRepo.Create(client.Client{
			Name: fmt.Sprintf("due-paused-%d", i), Enabled: true, QuotaResetPolicy: client.ResetNever, ExpiresAt: &due,
		})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, row.ID)
	}
	state.mu.Lock()
	state.runtimeVerificationUnknown = true
	state.mu.Unlock()

	logs := &lockedLogBuffer{}
	previousWriter := log.Writer()
	log.SetOutput(logs)
	defer log.SetOutput(previousWriter)

	reconciler := newExpirationReconciler(state)
	state.mu.Lock()
	state.expirationReconciler = reconciler
	state.mu.Unlock()
	reconciler.Start()
	defer reconciler.Stop()
	// Longer than expirationMinLoopWait, with several due clients: only the
	// paused short-circuit keeps this at a single line. The 1 s floor alone
	// would re-list the due set and log one refusal per client per second.
	time.Sleep(expirationMinLoopWait + 500*time.Millisecond)
	if n := strings.Count(logs.String(), "client expiry"); n != 1 {
		t.Fatalf("reconciler logged %d expiry lines in %v while paused with %d due clients; want exactly one pause line\n%s",
			n, expirationMinLoopWait+500*time.Millisecond, len(ids), logs.String())
	}
	var rows int
	if err := state.db.QueryRow(`SELECT COUNT(*) FROM expiration_enforcement`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("enforcement reserved %d rows while paused", rows)
	}

	state.mu.Lock()
	state.runtimeVerificationUnknown = false
	state.mu.Unlock()
	reconciler.Signal()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if err := state.db.QueryRow(`SELECT COUNT(DISTINCT client_id) FROM expiration_enforcement`).Scan(&rows); err != nil {
			t.Fatal(err)
		}
		if rows == len(ids) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("reconciler reserved enforcement for %d of %d clients after the pause cleared and it was signalled", rows, len(ids))
}

// Issue #1423 follow-up: the pause reminder is wall-clock based. Signals
// during a pause (client writes) must not add lines, and a pause that
// outlasts the reminder interval must log again.
func TestExpirationReconcilerPauseReminderIsWallClock(t *testing.T) {
	_, state := newApplyTrackedRouterWithState(t)
	state.expirationReconciler.Stop()
	due := time.Now().UTC().Unix() - 5
	if _, err := state.clientRepo.Create(client.Client{
		Name: "due-reminder", Enabled: true, QuotaResetPolicy: client.ResetNever, ExpiresAt: &due,
	}); err != nil {
		t.Fatal(err)
	}
	state.mu.Lock()
	state.runtimeVerificationUnknown = true
	state.mu.Unlock()

	logs := &lockedLogBuffer{}
	previousWriter := log.Writer()
	log.SetOutput(logs)
	defer log.SetOutput(previousWriter)

	reconciler := newExpirationReconciler(state)
	reconciler.pausedRetry = time.Hour
	reconciler.pausedReminder = 700 * time.Millisecond
	state.mu.Lock()
	state.expirationReconciler = reconciler
	state.mu.Unlock()
	reconciler.Start()
	defer reconciler.Stop()

	pauseLines := func() int { return strings.Count(logs.String(), "expiration enforcement paused") }
	// Many wakes inside one reminder interval: still exactly one line.
	for i := 0; i < 10; i++ {
		time.Sleep(30 * time.Millisecond)
		reconciler.Signal()
	}
	if n := pauseLines(); n != 1 {
		t.Fatalf("pause logged %d times across 10 signals within the reminder interval; want 1\n%s", n, logs.String())
	}
	// After the reminder interval, the next wake logs the reminder.
	time.Sleep(800 * time.Millisecond)
	reconciler.Signal()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && pauseLines() < 2 {
		time.Sleep(20 * time.Millisecond)
	}
	if n := pauseLines(); n != 2 {
		t.Fatalf("pause logged %d times after the reminder interval elapsed; want 2\n%s", n, logs.String())
	}
}

// Issue #1423 follow-up: clearing the pause resets the reminder clock, so a
// pause that re-enters well inside the reminder interval (two restores back
// to back) still logs on entry.
func TestExpirationReconcilerLogsEachPauseEntry(t *testing.T) {
	_, state := newApplyTrackedRouterWithState(t)
	state.expirationReconciler.Stop()
	due := time.Now().UTC().Unix() - 5
	row, err := state.clientRepo.Create(client.Client{
		Name: "due-reentry", Enabled: true, QuotaResetPolicy: client.ResetNever, ExpiresAt: &due,
	})
	if err != nil {
		t.Fatal(err)
	}
	setPaused := func(paused bool) {
		state.mu.Lock()
		state.runtimeVerificationUnknown = paused
		state.mu.Unlock()
	}
	setPaused(true)

	logs := &lockedLogBuffer{}
	previousWriter := log.Writer()
	log.SetOutput(logs)
	defer log.SetOutput(previousWriter)

	reconciler := newExpirationReconciler(state)
	reconciler.pausedRetry = time.Hour
	reconciler.pausedReminder = time.Hour
	state.mu.Lock()
	state.expirationReconciler = reconciler
	state.mu.Unlock()
	reconciler.Start()
	defer reconciler.Stop()

	pauseLines := func() int { return strings.Count(logs.String(), "expiration enforcement paused") }
	waitFor := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if cond() {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %s\n%s", what, logs.String())
	}
	waitFor("first pause line", func() bool { return pauseLines() == 1 })

	// Clear the pause and wait for an unpaused pass (it reserves enforcement).
	setPaused(false)
	reconciler.Signal()
	waitFor("an unpaused reconcile pass", func() bool {
		var rows int
		if err := state.db.QueryRow(`SELECT COUNT(*) FROM expiration_enforcement WHERE client_id=?`, row.ID).Scan(&rows); err != nil {
			t.Fatal(err)
		}
		return rows > 0
	})

	setPaused(true)
	reconciler.Signal()
	waitFor("a second pause-entry line", func() bool { return pauseLines() == 2 })
}
