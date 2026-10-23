package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/client"
)

func TestExpirationNextBoundaryWakesForAlreadyDueWork(t *testing.T) {
	_, state := newApplyTrackedRouterWithState(t)
	state.expirationReconciler.Stop()
	now := time.Now().UTC().Unix()
	due := now - 5
	future := now + 3600
	dueClient, err := state.clientRepo.Create(client.Client{
		Name: "due-expiry", Enabled: true, QuotaResetPolicy: client.ResetNever, ExpiresAt: &due,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.clientRepo.Create(client.Client{
		Name: "later-expiry", Enabled: true, QuotaResetPolicy: client.ResetNever, ExpiresAt: &future,
	}); err != nil {
		t.Fatal(err)
	}
	boundary, ok, err := state.expirationReconciler.nextBoundary(context.Background())
	if err != nil || !ok {
		t.Fatalf("nextBoundary due client: ok=%v err=%v", ok, err)
	}
	if boundary > now {
		t.Fatalf("nextBoundary=%d, want already-due <= %d", boundary, now)
	}

	retryAt := now - 1
	hash := fmt.Sprintf("%064x", 1)
	if _, err := state.db.Exec(`INSERT INTO expiration_enforcement(
		client_id,target_generation,target_payload_hash,target_expires_at,state,
		desired_revision,applied_revision,effective_at,next_retry_at,last_error,attempts,updated_at)
		VALUES(?,?,?,?,'failed',1,0,?,?, 'apply failed',1,?)`,
		dueClient.ID, dueClient.Version, hash, due, due, retryAt, now); err != nil {
		t.Fatal(err)
	}
	boundary, ok, err = state.expirationReconciler.nextBoundary(context.Background())
	if err != nil || !ok {
		t.Fatalf("nextBoundary due retry: ok=%v err=%v", ok, err)
	}
	if boundary > now {
		t.Fatalf("nextBoundary retry=%d, want already-due <= %d", boundary, now)
	}
}

func TestExpirationReservePinsEffectiveAtToSnapshotTime(t *testing.T) {
	_, state := newApplyTrackedRouterWithState(t)
	state.expirationReconciler.Stop()
	expired := time.Now().UTC().Add(-time.Minute).Unix()
	row, err := state.clientRepo.Create(client.Client{
		Name: "stale-effective", Enabled: true, QuotaResetPolicy: client.ResetNever, ExpiresAt: &expired,
	})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("client=%s;generation=%d;expires=%d;depleted=true", row.ID, row.Version, expired)))
	stale := expired - 3600
	state.mu.Lock()
	revision, done, err := state.reserveExpirationRevisionLocked(expiredClientCandidate{
		ID: row.ID, ExpiresAt: expired, CreatedAt: row.CreatedAt,
		TargetGeneration: int64(row.Version), TargetPayloadHash: hex.EncodeToString(digest[:]),
	}, stale)
	state.mu.Unlock()
	if err != nil || done || revision == 0 {
		t.Fatalf("reserve: revision=%d done=%v err=%v", revision, done, err)
	}
	payload, err := state.applySnapshots.Load(revision)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot managementSnapshot
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		t.Fatal(err)
	}
	if err := state.decryptSnapshot(&snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.EffectiveAt <= stale {
		t.Fatalf("EffectiveAt=%d reused stale listing time %d", snapshot.EffectiveAt, stale)
	}
	if snapshot.EffectiveAt < expired {
		t.Fatalf("EffectiveAt=%d before candidate expiry %d", snapshot.EffectiveAt, expired)
	}
}
