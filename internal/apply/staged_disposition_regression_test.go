package apply

import "testing"

func TestStagedDispositionDoesNotConsumeEnforcementConfirmation(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO clients(id,name,quota_reset_policy,notes,created_at,updated_at,version)
VALUES('client-staged','staged','never','',1,1,1)`); err != nil {
		t.Fatal(err)
	}
	revisions := NewRevisionStore(db)
	jobs := NewJobStore(db)
	desired, err := revisions.BumpDesired()
	if err != nil {
		t.Fatal(err)
	}
	const targetHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if _, err := db.Exec(`INSERT INTO expiration_enforcement
(client_id,target_generation,target_payload_hash,target_expires_at,state,desired_revision,applied_revision,effective_at,next_retry_at,last_error,attempts,updated_at)
VALUES('client-staged',1,?,100,'pending',?,0,100,0,'',0,1)`, targetHash, desired); err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(revisions, jobs, func(uint64) (Result, error) {
		return Result{
			Success:     true,
			Disposition: ApplyDispositionStaged,
			Confirmations: []EnforcementConfirmation{{
				Kind: "expiration", ClientID: "client-staged",
			}},
		}, nil
	})
	defer runner.Close()
	job, err := runner.Run(desired, "manual", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != StatusStaged {
		t.Fatalf("job status=%q, want staged", job.Status)
	}
	var state string
	var applied uint64
	if err := db.QueryRow(`SELECT state,applied_revision FROM expiration_enforcement WHERE client_id='client-staged'`).Scan(&state, &applied); err != nil {
		t.Fatal(err)
	}
	if state != "pending" || applied != 0 {
		t.Fatalf("staged-only consumed confirmation: state=%q applied=%d", state, applied)
	}
	current, err := revisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	if current.Applied != 0 || current.Desired != desired {
		t.Fatalf("staged-only changed revisions: %+v", current)
	}
}
