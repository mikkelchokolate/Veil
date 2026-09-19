package api

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/testutil/testdb"
)

func TestReconcilePanelUpdateJobsMatchesReleaseDisplayVersion(t *testing.T) {
	sha := strings.Repeat("a", 40)
	now := time.Now().UTC().Unix()
	cases := []struct {
		name            string
		status          string
		running         string
		updatedAt       int64
		wantStatus      string
		wantErrorSubstr string
	}{
		{
			name:       "restarting release binary succeeds",
			status:     "restarting",
			running:    "v0.6.3 (" + sha + ")",
			updatedAt:  now - 5,
			wantStatus: "succeeded",
		},
		{
			name:       "restart_pending release binary succeeds",
			status:     "restart_pending",
			running:    "v0.6.3 (" + sha + ")",
			updatedAt:  now - 5,
			wantStatus: "succeeded",
		},
		{
			name:       "wrong version stays in flight",
			status:     "restarting",
			running:    "v0.6.2 (" + sha + ")",
			updatedAt:  now - 10,
			wantStatus: "restarting",
		},
		{
			name:            "wrong version times out",
			status:          "restarting",
			running:         "v0.6.2 (" + sha + ")",
			updatedAt:       now - 400,
			wantStatus:      "failed",
			wantErrorSubstr: "panel restarted without expected version v0.6.3",
		},
		{
			name:       "exact tag match still succeeds",
			status:     "restarting",
			running:    "v0.6.3",
			updatedAt:  now - 5,
			wantStatus: "succeeded",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := &managementState{db: testdb.Open(t)}
			const jobID = "job-1"
			if _, err := state.db.Exec(
				`INSERT INTO panel_update_jobs(id,target_version,status,created_at,updated_at) VALUES(?,?,?,?,?)`,
				jobID, "v0.6.3", tc.status, tc.updatedAt, tc.updatedAt,
			); err != nil {
				t.Fatalf("insert job: %v", err)
			}
			state.reconcilePanelUpdateJobs(tc.running)
			job, err := state.getPanelUpdateJob(jobID)
			if err != nil {
				t.Fatalf("get job: %v", err)
			}
			if job.Status != tc.wantStatus {
				t.Fatalf("status=%q want %q error=%q", job.Status, tc.wantStatus, job.Error)
			}
			if tc.wantErrorSubstr != "" && !strings.Contains(job.Error, tc.wantErrorSubstr) {
				t.Fatalf("error=%q want substring %q", job.Error, tc.wantErrorSubstr)
			}
			if tc.wantErrorSubstr == "" && job.Error != "" && job.Status == "succeeded" {
				t.Fatalf("succeeded job has error %q", job.Error)
			}
		})
	}
}

func TestReconcilePanelUpdateJobsPrunesTerminalHistory(t *testing.T) {
	state := &managementState{db: testdb.Open(t)}
	now := time.Now().UTC().Unix()
	expired := now - panelUpdateJobRetentionSeconds - 60
	recent := now - 60
	insert := func(id, status string, updatedAt int64) {
		t.Helper()
		if _, err := state.db.Exec(
			`INSERT INTO panel_update_jobs(id,target_version,status,created_at,updated_at) VALUES(?,?,?,?,?)`,
			id, "v0.6.3", status, updatedAt, updatedAt,
		); err != nil {
			t.Fatalf("insert job %s: %v", id, err)
		}
	}
	insert("expired-succeeded", "succeeded", expired)
	insert("expired-failed", "failed", expired)
	insert("recent-succeeded", "succeeded", recent)
	insert("recent-failed", "failed", recent)
	// A stale in-flight job must be reconciled to a terminal status before it
	// becomes eligible for pruning; it is not deleted outright.
	insert("expired-staging", "staging", expired)

	state.reconcilePanelUpdateJobs("v0.6.2")

	for _, id := range []string{"expired-succeeded", "expired-failed"} {
		_, err := state.getPanelUpdateJob(id)
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("job %s: expected pruned row, got err=%v", id, err)
		}
	}
	for _, id := range []string{"recent-succeeded", "recent-failed", "expired-staging"} {
		if _, err := state.getPanelUpdateJob(id); err != nil {
			t.Fatalf("job %s: expected retained row: %v", id, err)
		}
	}
	job, err := state.getPanelUpdateJob("expired-staging")
	if err != nil || job.Status != "failed" {
		t.Fatalf("expired-staging: status=%q err=%v", job.Status, err)
	}
}
