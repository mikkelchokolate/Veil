//go:build unix

package api

import (
	"sync"
	"sync/atomic"
	"testing"
)

// Regression for the #1265 review residual: a Panel whole-store rewrite reads
// the journal (absorbDiskRevocationsLocked), then publishes a snapshot and
// removes or replaces the journal. r.mu is in-process only, so without the
// cross-process store lock a CLI revocation appended between that read and
// the remove was lost, and the next SIGHUP reload adopted the Panel's
// snapshot that still held the revoked session.
//
// The Panel side loops its rewrites as fast as it can while the CLI command
// runs once; afterwards a reload must never see the revoked session again.

func runPanelRewriteVsCLIRevokeRace(t *testing.T, rewrite func(*SessionRegistry) error, revoke func(statePath string) error) {
	t.Helper()
	const rounds = 40
	lost := 0
	for round := 0; round < rounds; round++ {
		statePath, panel, _ := cliRevocationFixture(t)
		alice := mustCreate(t, panel, "alice", "admin")

		var stop atomic.Bool
		var wg sync.WaitGroup
		var rewriteErr error
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !stop.Load() {
				panel.mu.Lock()
				err := rewrite(panel)
				panel.mu.Unlock()
				if err != nil {
					rewriteErr = err
					return
				}
			}
		}()
		err := revoke(statePath)
		stop.Store(true)
		wg.Wait()
		if err != nil {
			t.Fatal(err)
		}
		if rewriteErr != nil {
			t.Fatal(rewriteErr)
		}
		if err := panel.ReloadFromDisk(); err != nil {
			t.Fatal(err)
		}
		if _, ok := panel.Get(alice.Token); ok {
			lost++
			continue
		}
		if _, ok := restartedPanel(t, statePath, panel.now).Get(alice.Token); ok {
			lost++
		}
	}
	if lost > 0 {
		t.Fatalf("a concurrent Panel rewrite dropped the CLI revocation in %d/%d rounds", lost, rounds)
	}
}

func panelSnapshotRewrite(r *SessionRegistry) error { return r.saveLocked() }

func panelJournalCompaction(r *SessionRegistry) error { return r.compactJournalLocked() }

func cliRevokeAlice(statePath string) error {
	return RevokeUsernamePersistedSessions(statePath, "alice")
}

func TestPanelSnapshotRewriteRacingCLIUserRevocationKeepsIt(t *testing.T) {
	runPanelRewriteVsCLIRevokeRace(t, panelSnapshotRewrite, cliRevokeAlice)
}

func TestPanelSnapshotRewriteRacingCLIRevokeAllKeepsIt(t *testing.T) {
	runPanelRewriteVsCLIRevokeRace(t, panelSnapshotRewrite, RevokeAllPersistedSessions)
}

func TestPanelCompactionRacingCLIUserRevocationKeepsIt(t *testing.T) {
	runPanelRewriteVsCLIRevokeRace(t, panelJournalCompaction, cliRevokeAlice)
}

func TestPanelCompactionRacingCLIRevokeAllKeepsIt(t *testing.T) {
	runPanelRewriteVsCLIRevokeRace(t, panelJournalCompaction, RevokeAllPersistedSessions)
}
