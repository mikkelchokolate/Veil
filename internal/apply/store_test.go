package apply

import (
	"testing"
	"time"
)

func TestRevisionStoreStartsAtZero(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	rs := NewRevisionStore(db)

	rev, err := rs.Get()
	if err != nil {
		t.Fatalf("get revisions: %v", err)
	}
	if rev.Desired != 0 || rev.Applied != 0 {
		t.Fatalf("expected 0/0, got %d/%d", rev.Desired, rev.Applied)
	}
}

func TestBumpDesiredIncrementsAndKeepsApplied(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	rs := NewRevisionStore(db)

	d1, err := rs.BumpDesired()
	if err != nil {
		t.Fatalf("bump 1: %v", err)
	}
	d2, err := rs.BumpDesired()
	if err != nil {
		t.Fatalf("bump 2: %v", err)
	}
	if d1 != 1 || d2 != 2 {
		t.Fatalf("expected 1 then 2, got %d then %d", d1, d2)
	}
	rev, _ := rs.Get()
	if rev.Applied != 0 {
		t.Fatalf("applied must stay 0, got %d", rev.Applied)
	}
}

func TestMarkAppliedOnlyAdvancesApplied(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	rs := NewRevisionStore(db)

	d, _ := rs.BumpDesired()
	if err := rs.MarkApplied(d); err != nil {
		t.Fatalf("mark applied: %v", err)
	}
	rev, _ := rs.Get()
	if rev.Applied != d || rev.Desired != d {
		t.Fatalf("expected %d/%d, got %d/%d", d, d, rev.Desired, rev.Applied)
	}
}

func TestCatchUpAppliedAdvancesAppliedAndVerification(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	rs := NewRevisionStore(db)

	first, err := rs.BumpDesired()
	if err != nil {
		t.Fatal(err)
	}
	if err := rs.MarkApplied(first); err != nil {
		t.Fatal(err)
	}
	second, err := rs.BumpDesired()
	if err != nil {
		t.Fatal(err)
	}
	if err := rs.CatchUpApplied(second); err != nil {
		t.Fatalf("catch up: %v", err)
	}
	rev, err := rs.Get()
	if err != nil {
		t.Fatal(err)
	}
	if rev.Desired != second || rev.Applied != second {
		t.Fatalf("revisions=%+v want applied=%d", rev, second)
	}
	var verified uint64
	var status string
	if err := db.QueryRow(`SELECT verified_revision, status FROM runtime_verification WHERE id=1`).Scan(&verified, &status); err != nil {
		t.Fatal(err)
	}
	if verified != second || status != "verified" {
		t.Fatalf("verification verified=%d status=%q want %d/verified", verified, status, second)
	}
}

func TestMarkAppliedRejectsUnknownRevision(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	rs := NewRevisionStore(db)
	if _, err := rs.BumpDesired(); err != nil {
		t.Fatalf("bump: %v", err)
	}
	// Marking a revision ahead of desired is invalid.
	if err := rs.MarkApplied(99); err == nil {
		t.Fatalf("expected error marking applied revision beyond desired")
	}
}

func TestApplyJobLifecycle(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	js := NewJobStore(db)
	rs := NewRevisionStore(db)
	desired, _ := rs.BumpDesired()

	job := Job{
		ID:              "job-1",
		DesiredRevision: desired,
		BaseRevision:    0,
		Status:          StatusPending,
		Trigger:         "mutation",
		ActorID:         "admin",
		CreatedAt:       time.Now().Unix(),
	}
	if err := js.Create(job); err != nil {
		t.Fatalf("create job: %v", err)
	}

	got, err := js.Get("job-1")
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if got.Status != StatusPending || got.DesiredRevision != desired {
		t.Fatalf("unexpected job: %+v", got)
	}

	if err := js.MarkStatus("job-1", StatusApplying, "", ""); err != nil {
		t.Fatalf("mark applying: %v", err)
	}
	if err := js.Finish("job-1", StatusSucceeded, "", ""); err != nil {
		t.Fatalf("finish: %v", err)
	}
	got, _ = js.Get("job-1")
	if got.Status != StatusSucceeded || got.FinishedAt == nil {
		t.Fatalf("expected succeeded with finished_at, got %+v", got)
	}
}

func TestJobStoreListOrdersNewestFirst(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	js := NewJobStore(db)
	rs := NewRevisionStore(db)
	d, _ := rs.BumpDesired()
	createdAt := time.Now().Unix()
	for _, id := range []string{"z-old", "m-middle", "a-new"} {
		if err := js.Create(Job{ID: id, DesiredRevision: d, Status: StatusPending, Trigger: "mutation", CreatedAt: createdAt}); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	jobs, err := js.List(10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(jobs) != 3 {
		t.Fatalf("expected 3 jobs, got %d", len(jobs))
	}
	if jobs[0].ID != "a-new" {
		t.Fatalf("expected newest insertion first, got %s", jobs[0].ID)
	}
	latest, ok, err := js.LatestForRevision(d)
	if err != nil {
		t.Fatalf("latest for revision: %v", err)
	}
	if !ok || latest.ID != "a-new" {
		t.Fatalf("expected newest insertion for revision, got ok=%v job=%+v", ok, latest)
	}
}

func TestJobStorePersistsFailure(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	js := NewJobStore(db)
	rs := NewRevisionStore(db)
	d, _ := rs.BumpDesired()
	_ = js.Create(Job{ID: "j", DesiredRevision: d, Status: StatusPending, Trigger: "mutation", CreatedAt: time.Now().Unix()})
	if err := js.Finish("j", StatusFailed, "HEALTH_CHECK_FAILED", "unit veil-hysteria2 unhealthy"); err != nil {
		t.Fatalf("finish failed: %v", err)
	}
	got, _ := js.Get("j")
	if got.Status != StatusFailed || got.ErrorCode != "HEALTH_CHECK_FAILED" || got.ErrorMessage == "" {
		t.Fatalf("failure not persisted: %+v", got)
	}
}
