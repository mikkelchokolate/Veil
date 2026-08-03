package api

import (
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
	if s.runtimeVerificationUnknown || s.clientSubsystemStopping {
		s.mu.Unlock()
		return errors.New("quota enforcement paused while runtime verification is unknown")
	}
	if s.trafficCollector != nil {
		for _, health := range s.trafficCollector.ProviderHealth() {
			if health.State == "degraded" {
				s.mu.Unlock()
				return fmt.Errorf("quota enforcement paused while traffic provider %s is degraded", health.Key)
			}
		}
	}
	if mutation.TargetGeneration <= 0 || mutation.TargetPayloadHash == "" {
		current, currentErr := s.clientRepo.Get(mutation.ClientID)
		if currentErr != nil {
			s.mu.Unlock()
			return currentErr
		}
		mutation = client.BindQuotaTarget(current, mutation)
	}
	var desiredRevision, appliedRevision uint64
	var enforcementState string
	err := s.db.QueryRow(`SELECT state,desired_revision,applied_revision FROM quota_enforcement
WHERE client_id=? AND target_generation=? AND target_payload_hash=? AND state<>'superseded'`,
		mutation.ClientID, mutation.TargetGeneration, mutation.TargetPayloadHash).Scan(&enforcementState, &desiredRevision, &appliedRevision)
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
				result, err := tx.Exec(`UPDATE quota_enforcement SET state='applying',desired_revision=?,last_error='',next_retry_at=0,updated_at=?
WHERE client_id=? AND target_generation=? AND target_payload_hash=? AND state<>'superseded'`,
					revision, time.Now().UTC().Unix(), mutation.ClientID, mutation.TargetGeneration, mutation.TargetPayloadHash)
				if err != nil {
					return err
				}
				rows, err := result.RowsAffected()
				if err != nil {
					return err
				}
				if rows != 1 {
					return errors.New("quota enforcement target was superseded before revision binding")
				}
				_, err = tx.Exec(`UPDATE quota_enforcement SET superseded_revision=? WHERE client_id=? AND state='superseded' AND superseded_revision=0`, revision, mutation.ClientID)
				return err
			})
			return commitErr
		})
		if err != nil {
			s.mu.Unlock()
			return err
		}
	} else {
		_, err = s.db.Exec(`UPDATE quota_enforcement SET state='applying',updated_at=? WHERE client_id=? AND target_generation=? AND target_payload_hash=? AND desired_revision=? AND state<>'superseded'`,
			time.Now().UTC().Unix(), mutation.ClientID, mutation.TargetGeneration, mutation.TargetPayloadHash, desiredRevision)
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
WHERE client_id=? AND target_generation=? AND target_payload_hash=? AND desired_revision=? AND state<>'superseded'`, desiredRevision, time.Now().UTC().Unix(), mutation.ClientID, mutation.TargetGeneration, mutation.TargetPayloadHash, desiredRevision)
		return err
	}
	job, runErr := s.applyRunner.RunContextWithConfirmations(s.lifecycleContext(), desiredRevision, "quota", "system",
		veilapply.EnforcementConfirmation{Kind: "quota", ClientID: mutation.ClientID, TargetGeneration: mutation.TargetGeneration, TargetPayloadHash: mutation.TargetPayloadHash})
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
	_ = s.db.QueryRow(`SELECT attempts FROM quota_enforcement WHERE client_id=? AND target_generation=? AND target_payload_hash=?`, mutation.ClientID, mutation.TargetGeneration, mutation.TargetPayloadHash).Scan(&attempts)
	nextRetry := time.Now().UTC().Add(quotaRetryDelay(mutation.ClientID, attempts)).Unix()
	_, persistErr := s.db.Exec(`UPDATE quota_enforcement SET state='failed',last_error=?,next_retry_at=?,updated_at=?
WHERE client_id=? AND target_generation=? AND target_payload_hash=? AND desired_revision=? AND state<>'superseded'`, message, nextRetry, time.Now().UTC().Unix(), mutation.ClientID, mutation.TargetGeneration, mutation.TargetPayloadHash, desiredRevision)
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
