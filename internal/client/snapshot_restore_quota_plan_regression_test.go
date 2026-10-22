package client

import (
	"strings"
	"testing"
	"time"
)

// #1355: a rollback that restores a client which still has a quota, but with
// different quota bytes or a different reset policy/boundary than the live
// row, used to leave the live quota-enforcement record active. That record is
// bound to the old plan (its target_depleted / target_next_reset_at were
// computed under the old bytes and policy), so enforcement kept using the old
// values. A live plan change supersedes it (prepareQuotaResetTransition); the
// rollback must too, in the same transaction, so the reconciler replans
// against the restored plan.

type quotaRecord struct {
	state       string
	depleted    bool
	nextResetAt *int64
	superseded  int64
	desired     int64
}

func activeQuotaRecords(t *testing.T, repo *Repository, clientID string) []quotaRecord {
	t.Helper()
	rows, err := repo.db.Query(`SELECT state,target_depleted,target_next_reset_at,superseded_revision,desired_revision
FROM quota_enforcement WHERE client_id=? AND state<>'superseded'`, clientID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []quotaRecord
	for rows.Next() {
		var record quotaRecord
		var depleted int
		if err := rows.Scan(&record.state, &depleted, &record.nextResetAt, &record.superseded, &record.desired); err != nil {
			t.Fatal(err)
		}
		record.depleted = depleted != 0
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func restoreSnapshotClient(t *testing.T, repo *Repository, snapshot Client) {
	t.Helper()
	tx, err := repo.BeginTx()
	if err != nil {
		t.Fatal(err)
	}
	if err := ReplaceSnapshotTx(tx, []Client{snapshot}, nil, nil); err != nil {
		_ = tx.Rollback()
		t.Fatalf("ReplaceSnapshotTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestReplaceSnapshotTxQuotaBytesChangeReplacesEnforcementRecord(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	traffic := NewTrafficStore(db)
	small := int64(1000)
	live, err := repo.Create(Client{Name: "raised", Enabled: true, QuotaBytes: &small, QuotaResetPolicy: ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := repo.CreateBinding(Binding{ClientID: live.ID, InboundID: "hy2", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := traffic.RecordSample(Sample{BindingID: binding.ID, UploadBytes: 1200, AtUnix: 1}); err != nil {
		t.Fatal(err)
	}
	// Live: the 1000-byte quota is exhausted and its depleted target failed
	// to apply; it is backing off with a bound revision.
	if _, err := db.Exec(`UPDATE clients SET depleted=1 WHERE id=?`, live.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO quota_enforcement
  (client_id,target_generation,target_payload_hash,target_depleted,desired_revision,state,next_retry_at,updated_at)
  VALUES(?,?,?,1,7,'failed',?,1)`, live.ID, live.Version, strings.Repeat("c", 64), time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}

	// The selected revision carried a 10000-byte quota.
	snapshot := live
	large := int64(10000)
	snapshot.QuotaBytes = &large
	restoreSnapshotClient(t, repo, snapshot)

	if active := activeQuotaRecords(t, repo, live.ID); len(active) != 0 {
		t.Fatalf("enforcement record bound to the old 1000-byte plan is still active after restoring a 10000-byte quota: %+v", active)
	}
	var superseded int64
	if err := db.QueryRow(`SELECT superseded_revision FROM quota_enforcement WHERE client_id=? AND target_payload_hash=?`,
		live.ID, strings.Repeat("c", 64)).Scan(&superseded); err != nil {
		t.Fatal(err)
	}
	if superseded != 7 {
		t.Fatalf("superseded_revision=%d, want the record's desired revision 7", superseded)
	}

	// The replacement record is planned against the restored plan:
	// 1200 bytes used of 10000 is not depleted.
	reconciler := NewReconciler(repo, traffic, 0, nil)
	if _, err := reconciler.ReconcileOnce(); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	active := activeQuotaRecords(t, repo, live.ID)
	if len(active) != 1 {
		t.Fatalf("active enforcement records after replan = %+v, want exactly one", active)
	}
	if active[0].depleted {
		t.Fatalf("replacement enforcement record still targets depleted=true under the restored 10000-byte quota: %+v", active[0])
	}
	got, err := repo.Get(live.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.QuotaBytes == nil || *got.QuotaBytes != large || got.Depleted {
		t.Fatalf("client after replan quota=%v depleted=%v, want 10000/false", got.QuotaBytes, got.Depleted)
	}
}

func TestReplaceSnapshotTxQuotaPolicyChangeReplacesEnforcementRecord(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	traffic := NewTrafficStore(db)
	quota := int64(1 << 30)
	monthlyBoundary := int64(1_900_000_000)
	live, err := repo.Create(Client{
		Name: "policy", Enabled: true, QuotaBytes: &quota,
		QuotaResetPolicy: ResetMonthly, QuotaResetAt: &monthlyBoundary,
	})
	if err != nil {
		t.Fatal(err)
	}
	// A pending target bound to the live monthly plan.
	if _, err := db.Exec(`INSERT INTO quota_enforcement
  (client_id,target_generation,target_payload_hash,target_depleted,target_next_reset_at,state,updated_at)
  VALUES(?,?,?,0,?,'pending',1)`, live.ID, live.Version+1, strings.Repeat("d", 64), monthlyBoundary); err != nil {
		t.Fatal(err)
	}

	// The selected revision had a daily plan whose boundary the reconciler
	// had not scheduled yet.
	snapshot := live
	snapshot.QuotaResetPolicy = ResetDaily
	snapshot.QuotaResetAt = nil
	restoreSnapshotClient(t, repo, snapshot)

	if active := activeQuotaRecords(t, repo, live.ID); len(active) != 0 {
		t.Fatalf("enforcement record bound to the old monthly plan is still active after restoring a daily plan: %+v", active)
	}

	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	reconciler := NewReconciler(repo, traffic, 0, nil)
	reconciler.now = func() time.Time { return now }
	if _, err := reconciler.ReconcileOnce(); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	wantNext, err := nextQuotaBoundary(ResetDaily, now)
	if err != nil {
		t.Fatal(err)
	}
	active := activeQuotaRecords(t, repo, live.ID)
	if len(active) != 1 {
		t.Fatalf("active enforcement records after replan = %+v, want exactly one", active)
	}
	if active[0].nextResetAt == nil || *active[0].nextResetAt != wantNext {
		t.Fatalf("replacement record target_next_reset_at=%v, want the daily boundary %d (old monthly %d)",
			active[0].nextResetAt, wantNext, monthlyBoundary)
	}
	got, err := repo.Get(live.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.QuotaResetPolicy != ResetDaily || got.QuotaResetAt == nil || *got.QuotaResetAt != wantNext {
		t.Fatalf("client after replan policy=%q resetAt=%v, want daily/%d", got.QuotaResetPolicy, got.QuotaResetAt, wantNext)
	}
}

func TestReplaceSnapshotTxQuotaBoundaryChangeSupersedesEnforcementRecord(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	quota := int64(1 << 30)
	liveBoundary := int64(1_900_000_000)
	live, err := repo.Create(Client{
		Name: "boundary", Enabled: true, QuotaBytes: &quota,
		QuotaResetPolicy: ResetDaily, QuotaResetAt: &liveBoundary,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO quota_enforcement
  (client_id,target_generation,target_payload_hash,target_depleted,target_period_epoch,state,updated_at)
  VALUES(?,?,?,0,?,'pending',1)`, live.ID, live.Version+1, strings.Repeat("e", 64), liveBoundary); err != nil {
		t.Fatal(err)
	}
	snapshot := live
	snapshotBoundary := int64(1_899_000_000)
	snapshot.QuotaResetAt = &snapshotBoundary
	restoreSnapshotClient(t, repo, snapshot)
	if active := activeQuotaRecords(t, repo, live.ID); len(active) != 0 {
		t.Fatalf("enforcement record bound to the old reset boundary is still active: %+v", active)
	}
}

// Control: a rollback that restores the same quota plan must not drop the
// live enforcement record — like prepareQuotaResetTransition, only an
// effective plan change supersedes it.
func TestReplaceSnapshotTxUnchangedQuotaPlanKeepsEnforcementRecord(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	quota := int64(1000)
	boundary := int64(1_900_000_000)
	live, err := repo.Create(Client{
		Name: "same-plan", Enabled: true, QuotaBytes: &quota,
		QuotaResetPolicy: ResetWeekly, QuotaResetAt: &boundary,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE clients SET depleted=1 WHERE id=?`, live.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO quota_enforcement
  (client_id,target_generation,target_payload_hash,target_depleted,desired_revision,state,updated_at)
  VALUES(?,?,?,1,3,'applying',1)`, live.ID, live.Version, strings.Repeat("f", 64)); err != nil {
		t.Fatal(err)
	}
	snapshot := live
	snapshot.Depleted = false
	snapshot.Notes = "restored notes"
	// Distinct pointers with equal values: the comparison is by value.
	sameQuota, sameBoundary := quota, boundary
	snapshot.QuotaBytes, snapshot.QuotaResetAt = &sameQuota, &sameBoundary
	restoreSnapshotClient(t, repo, snapshot)

	active := activeQuotaRecords(t, repo, live.ID)
	if len(active) != 1 || active[0].state != "applying" || !active[0].depleted || active[0].desired != 3 {
		t.Fatalf("active enforcement records = %+v, want the untouched applying depleted target", active)
	}
	got, err := repo.Get(live.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Depleted || got.Notes != "restored notes" {
		t.Fatalf("restored client depleted=%v notes=%q, want true/%q", got.Depleted, got.Notes, "restored notes")
	}
}

// The policy half on its own: same bytes and boundary, different policy.
func TestReplaceSnapshotTxQuotaPolicyOnlyChangeSupersedesEnforcementRecord(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	quota := int64(1 << 30)
	boundary := int64(1_900_000_000)
	live, err := repo.Create(Client{
		Name: "policy-only", Enabled: true, QuotaBytes: &quota,
		QuotaResetPolicy: ResetWeekly, QuotaResetAt: &boundary,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO quota_enforcement
  (client_id,target_generation,target_payload_hash,target_depleted,state,updated_at)
  VALUES(?,?,?,0,'pending',1)`, live.ID, live.Version+1, strings.Repeat("9", 64)); err != nil {
		t.Fatal(err)
	}
	snapshot := live
	snapshot.QuotaResetPolicy = ResetMonthly
	restoreSnapshotClient(t, repo, snapshot)
	if active := activeQuotaRecords(t, repo, live.ID); len(active) != 0 {
		t.Fatalf("enforcement record bound to the old weekly policy is still active: %+v", active)
	}
}
