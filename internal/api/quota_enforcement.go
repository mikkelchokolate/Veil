package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"
	"time"

	veilapply "github.com/mikkelchokolate/Veil/internal/apply"
	"github.com/mikkelchokolate/Veil/internal/client"
)

func (s *managementState) enforceQuotaMutation(mutation client.QuotaMutation) error {
	s.mu.Lock()
	var desiredRevision, appliedRevision uint64
	var enforcementState string
	err := s.db.QueryRow(`SELECT state,desired_revision,applied_revision FROM quota_enforcement WHERE client_id=?`, mutation.ClientID).
		Scan(&enforcementState, &desiredRevision, &appliedRevision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		s.mu.Unlock()
		return err
	}
	if desiredRevision == 0 {
		err = s.trafficStore.WithRecordLock(func() error {
			var commitErr error
			desiredRevision, commitErr = s.commitClientMutationBoundLocked(func(tx *client.Tx) error {
				if mutation.ResetPeriod {
					if err := client.ResetQuotaPeriodTx(tx, mutation.ClientID); err != nil {
						return err
					}
				}
				current, err := tx.Get(mutation.ClientID)
				if err != nil {
					return err
				}
				current.Depleted = mutation.Depleted
				if mutation.NextResetAt != nil {
					next := *mutation.NextResetAt
					current.QuotaResetAt = &next
				}
				_, err = tx.Update(current, current.Version)
				return err
			}, func(tx *client.Tx, revision uint64) error {
				_, err := tx.Exec(`UPDATE quota_enforcement SET state='applying',desired_revision=?,last_error='',next_retry_at=0,updated_at=? WHERE client_id=?`,
					revision, time.Now().UTC().Unix(), mutation.ClientID)
				return err
			})
			return commitErr
		})
		if err != nil {
			s.mu.Unlock()
			return err
		}
	} else {
		_, err = s.db.Exec(`UPDATE quota_enforcement SET state='applying',updated_at=? WHERE client_id=? AND desired_revision=?`,
			time.Now().UTC().Unix(), mutation.ClientID, desiredRevision)
		if err != nil {
			s.mu.Unlock()
			return err
		}
	}
	revisions, err := s.applyRevisions.Get()
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if appliedRevision >= desiredRevision || revisions.Applied >= desiredRevision {
		_, err := s.db.Exec(`UPDATE quota_enforcement SET state='enforced',applied_revision=?,last_error='',next_retry_at=0,updated_at=?
WHERE client_id=? AND desired_revision=?`, desiredRevision, time.Now().UTC().Unix(), mutation.ClientID, desiredRevision)
		return err
	}
	job, runErr := s.applyRunner.RunContextWithConfirmations(context.Background(), desiredRevision, "quota", "system",
		veilapply.EnforcementConfirmation{Kind: "quota", ClientID: mutation.ClientID})
	if runErr == nil && job.Status == veilapply.StatusSucceeded {
		return nil
	}
	message := "quota runtime apply failed"
	if runErr != nil {
		message = runErr.Error()
	} else if job.ErrorMessage != "" {
		message = job.ErrorMessage
	}
	var attempts int
	_ = s.db.QueryRow(`SELECT attempts FROM quota_enforcement WHERE client_id=?`, mutation.ClientID).Scan(&attempts)
	nextRetry := time.Now().UTC().Add(quotaRetryDelay(mutation.ClientID, attempts)).Unix()
	_, persistErr := s.db.Exec(`UPDATE quota_enforcement SET state='failed',last_error=?,next_retry_at=?,updated_at=?
WHERE client_id=? AND desired_revision=?`, message, nextRetry, time.Now().UTC().Unix(), mutation.ClientID, desiredRevision)
	if persistErr != nil {
		return errors.Join(errors.New(message), persistErr)
	}
	return errors.New(message)
}

func quotaRetryDelay(clientID string, attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	exponent := attempts - 1
	if exponent > 8 {
		exponent = 8
	}
	base := time.Second * time.Duration(1<<exponent)
	if base > 5*time.Minute {
		base = 5 * time.Minute
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(fmt.Sprintf("%s:%d", clientID, attempts)))
	jitter := time.Duration(hash.Sum32()%250) * time.Millisecond
	return base + jitter
}
