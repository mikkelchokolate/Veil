package client

import (
	"errors"
	"testing"
)

// #1272 item 3: Service.Update used to supersede the quota enforcement record
// in autocommit before the version-checked UPDATE. A stale version then left
// the supersede committed for a client whose quota plan never changed. The
// supersede and the update must commit or roll back together.
func TestUpdateStaleVersionDoesNotCommitQuotaSupersede(t *testing.T) {
	for name, edit := range map[string]func(*Client){
		"quota cleared":  func(c *Client) { c.QuotaBytes = nil },
		"policy changed": func(c *Client) { c.QuotaResetPolicy = ResetMonthly },
	} {
		t.Run(name, func(t *testing.T) {
			db := openTestDB(t)
			defer db.Close()
			repo := NewRepository(db)
			svc := NewService(repo, NewCredentialStore(db, newTestCipher(t)))

			quota := int64(1000)
			resetAt := int64(1_700_000_000)
			created, err := repo.Create(Client{
				Name: "atomic-supersede", Enabled: true, QuotaBytes: &quota,
				QuotaResetPolicy: ResetDaily, QuotaResetAt: &resetAt, Depleted: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`INSERT INTO quota_enforcement
  (client_id,target_generation,target_payload_hash,target_depleted,target_period_epoch,state,updated_at)
  VALUES(?,1,printf('%064x',1),1,?, 'pending', 0)`, created.ID, resetAt); err != nil {
				t.Fatal(err)
			}

			stale := created
			edit(&stale)
			var state string
			assertPending := func(after string) {
				t.Helper()
				if err := db.QueryRow(`SELECT state FROM quota_enforcement WHERE client_id=?`, created.ID).Scan(&state); err != nil {
					t.Fatal(err)
				}
				if state != "pending" {
					t.Fatalf("enforcement state=%q after %s, want pending", state, after)
				}
			}
			if _, err := svc.Update(stale, created.Version+1); !errors.Is(err, ErrVersionConflict) {
				t.Fatalf("stale version: got %v, want ErrVersionConflict", err)
			}
			assertPending("a stale-version update")

			// Validation runs after the supersede; its rejection must roll
			// the supersede back too.
			invalid := stale
			invalid.Name = ""
			if _, err := svc.Update(invalid, created.Version); !errors.Is(err, ErrValidation) {
				t.Fatalf("invalid name: got %v, want ErrValidation", err)
			}
			assertPending("a rejected invalid update")

			// The same edit at the current version still supersedes.
			if _, err := svc.Update(stale, created.Version); err != nil {
				t.Fatalf("update at current version: %v", err)
			}
			if err := db.QueryRow(`SELECT state FROM quota_enforcement WHERE client_id=?`, created.ID).Scan(&state); err != nil {
				t.Fatal(err)
			}
			if state != "superseded" {
				t.Fatalf("enforcement state=%q after the update, want superseded", state)
			}
		})
	}
}
