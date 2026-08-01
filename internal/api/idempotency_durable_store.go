package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
)

type durableIdempotencyRecord struct {
	Fingerprint     string
	State           string
	Owner           string
	Status          int
	Headers         []byte
	Body            []byte
	Generation      uint64
	Encrypted       bool
	Actor           string
	Endpoint        string
	AuthGeneration  string
	ReplayExpiresAt int64
	ResultID        string
}

const secretReplayTTL = 5 * time.Minute

type secretReplayEnvelope struct {
	Actor          string `json:"actor"`
	AuthGeneration string `json:"authGeneration"`
	Endpoint       string `json:"endpoint"`
	Body           []byte `json:"body"`
}

func idempotencyOwnerID() string {
	return fmt.Sprintf("pid:%d:%s", os.Getpid(), uuid.NewString())
}

func (s *idempotencyStore) serveDurable(w http.ResponseWriter, r *http.Request, next http.Handler, key, scope, fingerprint string) {
	if s.durableClosed() {
		writeError(w, "idempotency service unavailable", http.StatusServiceUnavailable)
		return
	}
	owner, record, err := s.reserveDurable(r, key, scope, fingerprint)
	if err != nil {
		writeError(w, "idempotency service unavailable", http.StatusServiceUnavailable)
		return
	}
	if !owner {
		if record.Fingerprint != fingerprint {
			writeError(w, "Idempotency-Key was already used with a different request", http.StatusConflict)
			return
		}
		if record.State != "completed" {
			record, err = s.waitDurable(r, scope, fingerprint)
			if err != nil {
				if errors.Is(err, errIdempotencyWaitCanceled) {
					writeError(w, "request canceled while waiting for idempotent operation", http.StatusRequestTimeout)
				} else {
					w.Header().Set("Retry-After", "1")
					writeError(w, "idempotent operation is still pending", http.StatusConflict)
				}
				return
			}
		}
		s.replayDurableIdempotencyResponse(w, r, record)
		return
	}

	capture := newBufferedResponse(w.Header())
	next.ServeHTTP(capture, r)
	status := capture.status
	if status == 0 {
		status = http.StatusOK
	}
	storedStatus := status
	storedBody := capture.body.Bytes()
	if len(storedBody) > maxIdempotencyBody {
		storedStatus = http.StatusAccepted
		storedBody = []byte(`{"status":"committed","result":"response_too_large"}`)
	}
	// Completion is durable for every handler outcome. A post-commit read,
	// encoding, or transport failure must be replayed rather than deleting the
	// reservation and executing the mutation again.
	if err := s.completeDurable(scope, fingerprint, record, storedStatus, capture.header, storedBody); err != nil {
		writeError(w, "failed to finalize idempotent response", http.StatusServiceUnavailable)
		return
	}
	if status >= 200 && status < 400 && (r.Method == http.MethodDelete || strings.HasSuffix(r.URL.Path, "/rotate")) {
		_ = s.invalidateSecretReplayEnvelopes(record.Actor, scope)
	}
	copyHTTPHeader(w.Header(), capture.header)
	w.WriteHeader(status)
	_, _ = w.Write(capture.body.Bytes())
}

var errIdempotencyWaitCanceled = errors.New("idempotency wait canceled")

func (s *idempotencyStore) durableClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *idempotencyStore) reserveDurable(r *http.Request, key, scope, fingerprint string) (bool, durableIdempotencyRecord, error) {
	now := s.now().UTC()
	nowUnix := now.Unix()
	_, _ = s.db.Exec(`DELETE FROM idempotency_records WHERE expires_at<=? AND state='completed'`, nowUnix)
	actor := actorFromRequest(r)
	if actor == "" {
		actor = clientIP(r)
	}
	endpoint := r.Method + " " + r.URL.EscapedPath()
	authGeneration := idempotencyAuthGeneration(r)
	result, err := s.db.Exec(`INSERT OR IGNORE INTO idempotency_records
	  (scope, actor_id, auth_generation, endpoint, idempotency_key, payload_hash, state, owner_process,
	   reserved_until, created_at, updated_at, expires_at, operation_generation)
	  VALUES(?,?,?,?,?,?,'reserved',?,?,?,?,?,1)`,
		scope, actor, authGeneration, endpoint, key, fingerprint, s.owner,
		now.Add(2*time.Minute).Unix(), nowUnix, nowUnix, now.Add(idempotencyTTL).Unix())
	if err != nil {
		return false, durableIdempotencyRecord{}, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, durableIdempotencyRecord{}, err
	}
	if rows == 1 {
		return true, durableIdempotencyRecord{Fingerprint: fingerprint, State: "reserved", Owner: s.owner,
			Generation: 1, Actor: actor, Endpoint: endpoint, AuthGeneration: authGeneration}, nil
	}
	var takeover durableIdempotencyRecord
	err = s.db.QueryRow(`UPDATE idempotency_records SET owner_process=?,reserved_until=?,updated_at=?,
 operation_generation=operation_generation+1
WHERE scope=? AND payload_hash=? AND state='reserved' AND reserved_until<=?
RETURNING payload_hash,state,owner_process,operation_generation,actor_id,endpoint,auth_generation`,
		s.owner, now.Add(2*time.Minute).Unix(), nowUnix, scope, fingerprint, nowUnix).
		Scan(&takeover.Fingerprint, &takeover.State, &takeover.Owner, &takeover.Generation,
			&takeover.Actor, &takeover.Endpoint, &takeover.AuthGeneration)
	if err == nil {
		return true, takeover, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, durableIdempotencyRecord{}, err
	}
	record, err := s.readDurable(scope)
	return false, record, err
}

func (s *idempotencyStore) readDurable(scope string) (durableIdempotencyRecord, error) {
	var record durableIdempotencyRecord
	err := s.db.QueryRow(`SELECT payload_hash, state, owner_process,
	  COALESCE(response_status,0), COALESCE(response_headers,X''), COALESCE(response_body,X''),
	  operation_generation,response_encrypted,actor_id,endpoint,auth_generation,replay_expires_at,result_record_id
	  FROM idempotency_records WHERE scope=?`, scope).
		Scan(&record.Fingerprint, &record.State, &record.Owner, &record.Status, &record.Headers, &record.Body,
			&record.Generation, &record.Encrypted, &record.Actor, &record.Endpoint, &record.AuthGeneration,
			&record.ReplayExpiresAt, &record.ResultID)
	return record, err
}

func (s *idempotencyStore) waitDurable(r *http.Request, scope, fingerprint string) (durableIdempotencyRecord, error) {
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-r.Context().Done():
			return durableIdempotencyRecord{}, errIdempotencyWaitCanceled
		case <-timer.C:
			return durableIdempotencyRecord{}, errors.New("idempotency reservation timeout")
		case <-ticker.C:
			record, err := s.readDurable(scope)
			if errors.Is(err, sql.ErrNoRows) {
				return durableIdempotencyRecord{}, errors.New("idempotency reservation disappeared")
			}
			if err != nil {
				return durableIdempotencyRecord{}, err
			}
			if record.Fingerprint != fingerprint {
				return record, nil
			}
			if record.State == "completed" {
				return record, nil
			}
		}
	}
}

func (s *idempotencyStore) completeDurable(scope, fingerprint string, record durableIdempotencyRecord, status int, headers http.Header, body []byte) error {
	storedHeaders := headers.Clone()
	storedHeaders.Del("X-Request-ID")
	storedHeaders.Del("Set-Cookie")
	encodedHeaders, err := json.Marshal(storedHeaders)
	if err != nil {
		return err
	}
	now := s.now().UTC()
	generation := record.Generation
	storedBody := append([]byte(nil), body...)
	encrypted := 0
	expiresAt := now.Add(idempotencyTTL)
	replayExpiresAt := int64(0)
	if isSecretReplayEndpoint(record.Endpoint) {
		expiresAt = now.Add(secretReplayTTL)
		replayExpiresAt = expiresAt.Unix()
		encrypted = 1
		if s.replayCipher == nil {
			storedBody = nil
		} else {
			envelope, err := json.Marshal(secretReplayEnvelope{Actor: record.Actor, AuthGeneration: record.AuthGeneration,
				Endpoint: record.Endpoint, Body: storedBody})
			if err != nil {
				return err
			}
			sealed, err := s.replayCipher.Encrypt(string(envelope))
			if err != nil {
				return err
			}
			storedBody = []byte(sealed)
		}
	}
	resultID := uuid.NewString()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO idempotency_results
  (id,scope,operation_generation,response_status,response_headers,response_body,encrypted,created_at,expires_at)
  VALUES(?,?,?,?,?,?,?,?,?)`, resultID, scope, generation, status, encodedHeaders, storedBody, encrypted,
		now.Unix(), expiresAt.Unix()); err != nil {
		return err
	}
	result, err := tx.Exec(`UPDATE idempotency_records SET
  state='completed', response_status=?, response_headers=?, response_body=?,
  updated_at=?, expires_at=?,result_record_id=?,response_encrypted=?,replay_expires_at=?
  WHERE scope=? AND payload_hash=? AND owner_process=? AND operation_generation=? AND state='reserved'`,
		status, encodedHeaders, storedBody, now.Unix(), expiresAt.Unix(), resultID, encrypted, replayExpiresAt,
		scope, fingerprint, s.owner, generation)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return errors.New("idempotency reservation ownership lost")
	}
	return tx.Commit()
}

func (s *idempotencyStore) invalidateSecretReplayEnvelopes(actor, exceptScope string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE idempotency_results SET response_body=NULL
WHERE id IN (SELECT result_record_id FROM idempotency_records
             WHERE actor_id=? AND scope<>? AND response_encrypted=1)`, actor, exceptScope); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE idempotency_records SET response_body=NULL,replay_expires_at=?
WHERE actor_id=? AND scope<>? AND response_encrypted=1`, s.now().UTC().Unix(), actor, exceptScope); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *idempotencyStore) replayDurableIdempotencyResponse(w http.ResponseWriter, r *http.Request, record durableIdempotencyRecord) {
	headers := make(http.Header)
	if len(record.Headers) > 0 {
		_ = json.Unmarshal(record.Headers, &headers)
	}
	copyHTTPHeader(w.Header(), headers)
	w.Header().Set("Idempotency-Replayed", "true")
	status := record.Status
	if status == 0 {
		status = http.StatusOK
	}
	body := record.Body
	if record.Encrypted {
		if record.ReplayExpiresAt <= s.now().UTC().Unix() || s.replayCipher == nil || len(body) == 0 {
			writeError(w, "one-time secret replay envelope expired", http.StatusGone)
			return
		}
		plaintext, err := s.replayCipher.Decrypt(string(body))
		if err != nil {
			writeError(w, "one-time secret replay envelope unavailable", http.StatusGone)
			return
		}
		var envelope secretReplayEnvelope
		if err := json.Unmarshal([]byte(plaintext), &envelope); err != nil || envelope.Actor != record.Actor ||
			envelope.AuthGeneration != idempotencyAuthGeneration(r) || envelope.Endpoint != r.Method+" "+r.URL.EscapedPath() {
			writeError(w, "one-time secret replay envelope scope mismatch", http.StatusForbidden)
			return
		}
		body = envelope.Body
	}
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func isSecretReplayEndpoint(endpoint string) bool {
	return strings.Contains(endpoint, "/credentials/") || strings.Contains(endpoint, "/tokens")
}
