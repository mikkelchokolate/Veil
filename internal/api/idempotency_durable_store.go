package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
)

type durableIdempotencyRecord struct {
	Fingerprint      string
	State            string
	Owner            string
	Status           int
	Headers          []byte
	Body             []byte
	Generation       uint64
	Encrypted        bool
	Actor            string
	Endpoint         string
	AuthGeneration   string
	ReplayExpiresAt  int64
	ResultID         string
	OperationID      string
	HeartbeatAt      int64
	ReservedUntil    int64
	Sensitivity      string
	ResourceID       string
	SecretGeneration uint64
}

const secretReplayTTL = 5 * time.Minute

const (
	idempotencyOutcomeHeader          = "X-Veil-Internal-Idempotency-Outcome"
	idempotencySensitivityHeader      = "X-Veil-Internal-Response-Sensitivity"
	idempotencyResourceHeader         = "X-Veil-Internal-Resource-ID"
	idempotencySecretGenerationHeader = "X-Veil-Internal-Secret-Generation"
)

type secretReplayEnvelope struct {
	Actor            string `json:"actor"`
	AuthGeneration   string `json:"authGeneration"`
	Endpoint         string `json:"endpoint"`
	ResourceID       string `json:"resourceId"`
	SecretGeneration uint64 `json:"secretGeneration"`
	OperationID      string `json:"operationId"`
	Body             []byte `json:"body"`
}

func markIdempotencySecretResponse(w http.ResponseWriter, resourceID string, secretGeneration uint64) {
	w.Header().Set(idempotencySensitivityHeader, "secret")
	w.Header().Set(idempotencyResourceHeader, resourceID)
	w.Header().Set(idempotencySecretGenerationHeader, strconv.FormatUint(secretGeneration, 10))
}

func idempotencyOwnerID() string {
	return fmt.Sprintf("pid:%d:%s", os.Getpid(), uuid.NewString())
}

func idempotencyOwnerAlive(owner string) bool {
	parts := strings.SplitN(owner, ":", 3)
	if len(parts) != 3 || parts[0] != "pid" {
		return false
	}
	pid, err := strconv.Atoi(parts[1])
	if err != nil || pid <= 0 {
		return false
	}
	err = syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
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
	r = r.WithContext(withIdempotencyDomainOperation(r.Context(), idempotencyDomainOperation{ID: record.OperationID, Scope: scope, Generation: record.Generation}))
	stopHeartbeat := s.startDurableHeartbeat(scope, fingerprint, record)
	next.ServeHTTP(capture, r)
	stopHeartbeat()
	status := capture.status
	if status == 0 {
		status = http.StatusOK
	}
	outcome := classifyIdempotencyOutcome(capture.header, status)
	record.Sensitivity = capture.header.Get(idempotencySensitivityHeader)
	record.ResourceID = capture.header.Get(idempotencyResourceHeader)
	if rawGeneration := capture.header.Get(idempotencySecretGenerationHeader); rawGeneration != "" {
		record.SecretGeneration, _ = strconv.ParseUint(rawGeneration, 10, 64)
	}
	for _, internalHeader := range []string{idempotencyOutcomeHeader, idempotencySensitivityHeader, idempotencyResourceHeader, idempotencySecretGenerationHeader} {
		capture.header.Del(internalHeader)
	}
	responseStatus := status
	responseBody := append([]byte(nil), capture.body.Bytes()...)
	if capture.overflow {
		responseStatus = http.StatusAccepted
		responseBody = []byte(`{"status":"committed","result":"response_too_large"}`)
		capture.header.Set("Content-Type", "application/json")
		outcome = "committed_response_pending"
	}
	if outcome == "not_started_retryable" {
		_ = s.abortDurable(scope, fingerprint, record)
		copyHTTPHeader(w.Header(), capture.header)
		w.WriteHeader(responseStatus)
		_, _ = w.Write(responseBody)
		return
	}
	if err := s.completeDurable(scope, fingerprint, record, responseStatus, capture.header, responseBody, outcome); err != nil {
		writeError(w, "failed to finalize idempotent response", http.StatusServiceUnavailable)
		return
	}
	if responseStatus >= 200 && responseStatus < 400 && (r.Method == http.MethodDelete || strings.HasSuffix(r.URL.Path, "/rotate")) {
		_ = s.invalidateSecretReplayEnvelopes(record.Actor, scope)
	}
	copyHTTPHeader(w.Header(), capture.header)
	w.WriteHeader(responseStatus)
	_, _ = w.Write(responseBody)
}

var errIdempotencyWaitCanceled = errors.New("idempotency wait canceled")

func classifyIdempotencyOutcome(header http.Header, status int) string {
	if explicit := strings.TrimSpace(header.Get(idempotencyOutcomeHeader)); explicit != "" {
		switch explicit {
		case "validation_terminal", "not_started_retryable", "committed", "committed_response_pending", "terminal_success":
			return explicit
		}
	}
	if status >= 500 {
		return "not_started_retryable"
	}
	if status >= 400 {
		return "validation_terminal"
	}
	return "committed"
}

func (s *idempotencyStore) abortDurable(scope, fingerprint string, record durableIdempotencyRecord) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE domain_operations SET state='abandoned' WHERE id=? AND scope=? AND operation_generation=? AND state='reserved'`, record.OperationID, scope, record.Generation); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM idempotency_records WHERE scope=? AND payload_hash=? AND owner_process=? AND operation_generation=? AND state='reserved'`, scope, fingerprint, s.owner, record.Generation); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.signalDurableNotification(scope)
	return nil
}

func (s *idempotencyStore) durableClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *idempotencyStore) cleanupDurableIdempotency(nowUnix int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM idempotency_records WHERE expires_at<=? AND state='completed'`, nowUnix); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM domain_operations WHERE state IN ('committed','abandoned') AND (result_record_id IS NULL OR result_record_id IN (SELECT id FROM idempotency_results WHERE expires_at<=?))`, nowUnix); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM idempotency_results WHERE expires_at<=? AND NOT EXISTS (SELECT 1 FROM domain_operations d WHERE d.result_record_id=idempotency_results.id)`, nowUnix); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *idempotencyStore) reserveDurable(r *http.Request, key, scope, fingerprint string) (bool, durableIdempotencyRecord, error) {
	now := s.now().UTC()
	nowUnix := now.Unix()
	leaseTTL := s.reservationTTL
	if leaseTTL <= 0 {
		leaseTTL = 2 * time.Minute
	}
	reservedUntil := now.Add(leaseTTL).Unix()
	_ = s.cleanupDurableIdempotency(nowUnix)
	actor := actorFromRequest(r)
	if actor == "" {
		actor = clientIP(r)
	}
	endpoint := r.Method + " " + r.URL.EscapedPath()
	authGeneration := idempotencyAuthGeneration(r)
	operationID := uuid.NewString()
	tx, err := s.db.Begin()
	if err != nil {
		return false, durableIdempotencyRecord{}, err
	}
	result, err := tx.Exec(`INSERT OR IGNORE INTO idempotency_records
  (scope, actor_id, auth_generation, endpoint, idempotency_key, payload_hash, state, owner_process,
   reserved_until, heartbeat_at, created_at, updated_at, expires_at, operation_generation,domain_operation_id)
  VALUES(?,?,?,?,?,?,'reserved',?,?,?,?,?,?,1,?)`,
		scope, actor, authGeneration, endpoint, key, fingerprint, s.owner,
		now.Add(leaseTTL).Unix(), nowUnix, nowUnix, nowUnix, now.Add(idempotencyTTL).Unix(), operationID)
	if err != nil {
		_ = tx.Rollback()
		return false, durableIdempotencyRecord{}, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		_ = tx.Rollback()
		return false, durableIdempotencyRecord{}, err
	}
	if rows == 1 {
		if _, err := tx.Exec(`INSERT INTO domain_operations(id,scope,operation_generation,state,created_at) VALUES(?,?,1,'reserved',?)`, operationID, scope, nowUnix); err != nil {
			_ = tx.Rollback()
			return false, durableIdempotencyRecord{}, err
		}
		if err := tx.Commit(); err != nil {
			return false, durableIdempotencyRecord{}, err
		}
		return true, durableIdempotencyRecord{Fingerprint: fingerprint, State: "reserved", Owner: s.owner,
			Generation: 1, Actor: actor, Endpoint: endpoint, AuthGeneration: authGeneration,
			OperationID: operationID, HeartbeatAt: nowUnix, ReservedUntil: now.Add(leaseTTL).Unix()}, nil
	}
	if err := tx.Commit(); err != nil {
		return false, durableIdempotencyRecord{}, err
	}
	current, err := s.readDurable(scope)
	if err != nil {
		return false, durableIdempotencyRecord{}, err
	}
	if current.Fingerprint != fingerprint || current.State != "reserved" {
		return false, current, nil
	}
	if idempotencyOwnerAlive(current.Owner) {
		return false, current, nil
	}
	var domainState, domainResult string
	if current.OperationID != "" {
		_ = s.db.QueryRow(`SELECT state,domain_result_json FROM domain_operations WHERE id=? AND scope=? AND operation_generation=?`, current.OperationID, scope, current.Generation).Scan(&domainState, &domainResult)
	}
	if domainState == "mutation_committed" {
		result, err := s.db.Exec(`UPDATE idempotency_records SET owner_process=?,heartbeat_at=?,reserved_until=?,updated_at=?
WHERE scope=? AND payload_hash=? AND owner_process=? AND operation_generation=? AND state='reserved'`,
			s.owner, nowUnix, reservedUntil, nowUnix, scope, fingerprint, current.Owner, current.Generation)
		if err != nil {
			return false, durableIdempotencyRecord{}, err
		}
		rows, _ := result.RowsAffected()
		if rows != 1 {
			record, readErr := s.readDurable(scope)
			return false, record, readErr
		}
		current.Owner = s.owner
		body := []byte(domainResult)
		if len(body) == 0 {
			body = []byte(`{"status":"committed_response_pending"}`)
		}
		if err := s.completeDurable(scope, fingerprint, current, http.StatusAccepted,
			http.Header{"Content-Type": []string{"application/json"}}, body, "committed_response_pending"); err != nil {
			return false, durableIdempotencyRecord{}, err
		}
		recovered, err := s.readDurable(scope)
		return false, recovered, err
	}
	if current.ReservedUntil > nowUnix {
		return false, current, nil
	}
	operationID = uuid.NewString()
	tx, err = s.db.Begin()
	if err != nil {
		return false, durableIdempotencyRecord{}, err
	}
	defer tx.Rollback()
	var takeover durableIdempotencyRecord
	err = tx.QueryRow(`UPDATE idempotency_records SET owner_process=?,reserved_until=?,heartbeat_at=?,updated_at=?,
 operation_generation=operation_generation+1,domain_operation_id=?
WHERE scope=? AND payload_hash=? AND state='reserved' AND reserved_until<=? AND owner_process=?
RETURNING payload_hash,state,owner_process,operation_generation,actor_id,endpoint,auth_generation,domain_operation_id,heartbeat_at,reserved_until`,
		s.owner, now.Add(leaseTTL).Unix(), nowUnix, nowUnix, operationID, scope, fingerprint, nowUnix, current.Owner).
		Scan(&takeover.Fingerprint, &takeover.State, &takeover.Owner, &takeover.Generation,
			&takeover.Actor, &takeover.Endpoint, &takeover.AuthGeneration, &takeover.OperationID, &takeover.HeartbeatAt, &takeover.ReservedUntil)
	if errors.Is(err, sql.ErrNoRows) {
		_ = tx.Rollback()
		record, readErr := s.readDurable(scope)
		return false, record, readErr
	}
	if err != nil {
		return false, durableIdempotencyRecord{}, err
	}
	if _, err := tx.Exec(`UPDATE domain_operations SET state='abandoned' WHERE id=? AND state='reserved'`, current.OperationID); err != nil {
		return false, durableIdempotencyRecord{}, err
	}
	if _, err := tx.Exec(`INSERT INTO domain_operations(id,scope,operation_generation,state,created_at) VALUES(?,?,?,'reserved',?)`, operationID, scope, takeover.Generation, nowUnix); err != nil {
		return false, durableIdempotencyRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return false, durableIdempotencyRecord{}, err
	}
	return true, takeover, nil
}

func (s *idempotencyStore) readDurable(scope string) (durableIdempotencyRecord, error) {
	var record durableIdempotencyRecord
	err := s.db.QueryRow(`SELECT r.payload_hash, r.state, r.owner_process,
  COALESCE(res.response_status,0), COALESCE(res.response_headers,X''), COALESCE(res.response_body,X''),
  r.operation_generation,COALESCE(res.encrypted,0),r.actor_id,r.endpoint,r.auth_generation,r.replay_expires_at,r.result_record_id,
  r.domain_operation_id,r.heartbeat_at,r.reserved_until,r.sensitivity,r.resource_id,r.secret_generation
  FROM idempotency_records r
  LEFT JOIN idempotency_results res ON res.id=r.result_record_id
  WHERE r.scope=?`, scope).
		Scan(&record.Fingerprint, &record.State, &record.Owner, &record.Status, &record.Headers, &record.Body,
			&record.Generation, &record.Encrypted, &record.Actor, &record.Endpoint, &record.AuthGeneration,
			&record.ReplayExpiresAt, &record.ResultID, &record.OperationID, &record.HeartbeatAt, &record.ReservedUntil,
			&record.Sensitivity, &record.ResourceID, &record.SecretGeneration)
	return record, err
}

func (s *idempotencyStore) startDurableHeartbeat(scope, fingerprint string, record durableIdempotencyRecord) func() {
	stop := make(chan struct{})
	done := make(chan struct{})
	leaseTTL := s.reservationTTL
	if leaseTTL <= 0 {
		leaseTTL = 2 * time.Minute
	}
	interval := leaseTTL / 3
	if interval > 10*time.Second {
		interval = 10 * time.Second
	}
	if interval < 50*time.Millisecond {
		interval = 50 * time.Millisecond
	}
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				now := s.now().UTC()
				_, _ = s.db.Exec(`UPDATE idempotency_records SET heartbeat_at=?,reserved_until=?,updated_at=?
WHERE scope=? AND payload_hash=? AND owner_process=? AND operation_generation=? AND state='reserved'`,
					now.Unix(), now.Add(leaseTTL).Unix(), now.Unix(), scope, fingerprint, s.owner, record.Generation)
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() { close(stop); <-done })
	}
}

func (s *idempotencyStore) durableNotification(scope string) <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if notification := s.notifications[scope]; notification != nil {
		return notification
	}
	notification := make(chan struct{})
	s.notifications[scope] = notification
	return notification
}

func (s *idempotencyStore) signalDurableNotification(scope string) {
	s.mu.Lock()
	if notification := s.notifications[scope]; notification != nil {
		close(notification)
	}
	s.notifications[scope] = make(chan struct{})
	s.mu.Unlock()
}

func (s *idempotencyStore) waitDurable(r *http.Request, scope, fingerprint string) (durableIdempotencyRecord, error) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	notification := s.durableNotification(scope)
	for {
		select {
		case <-r.Context().Done():
			return durableIdempotencyRecord{}, errIdempotencyWaitCanceled
		case <-timer.C:
			return durableIdempotencyRecord{}, errors.New("idempotency reservation timeout")
		case <-notification:
			notification = s.durableNotification(scope)
		case <-ticker.C:
		}
		record, err := s.readDurable(scope)
		if errors.Is(err, sql.ErrNoRows) {
			return durableIdempotencyRecord{}, errors.New("idempotency reservation disappeared")
		}
		if err != nil {
			return durableIdempotencyRecord{}, err
		}
		if record.Fingerprint != fingerprint || record.State == "completed" {
			return record, nil
		}
	}
}

func (s *idempotencyStore) completeDurable(scope, fingerprint string, record durableIdempotencyRecord, status int, headers http.Header, body []byte, outcome string) error {
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
	if record.Sensitivity == "secret" {
		expiresAt = now.Add(secretReplayTTL)
		replayExpiresAt = expiresAt.Unix()
		encrypted = 1
		if s.replayCipher == nil {
			storedBody = nil
		} else {
			envelope, err := json.Marshal(secretReplayEnvelope{
				Actor: record.Actor, AuthGeneration: record.AuthGeneration, Endpoint: record.Endpoint,
				ResourceID: record.ResourceID, SecretGeneration: record.SecretGeneration, OperationID: record.OperationID,
				Body: storedBody,
			})
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
  state='completed', response_status=NULL, response_headers=NULL, response_body=NULL,
  updated_at=?, expires_at=?,result_record_id=?,response_encrypted=?,replay_expires_at=?,
  outcome_class=?,sensitivity=?,resource_id=?,secret_generation=?
  WHERE scope=? AND payload_hash=? AND owner_process=? AND operation_generation=? AND domain_operation_id=? AND state='reserved'`,
		now.Unix(), expiresAt.Unix(), resultID, encrypted, replayExpiresAt,
		outcome, record.Sensitivity, record.ResourceID, record.SecretGeneration,
		scope, fingerprint, s.owner, generation, record.OperationID)
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
	if _, err := tx.Exec(`UPDATE domain_operations SET state='committed',result_record_id=?,committed_at=? WHERE id=? AND scope=? AND operation_generation=? AND state IN ('reserved','mutation_committed')`,
		resultID, now.Unix(), record.OperationID, scope, generation); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.signalDurableNotification(scope)
	return nil
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
			envelope.AuthGeneration != idempotencyAuthGeneration(r) || envelope.Endpoint != r.Method+" "+r.URL.EscapedPath() ||
			envelope.ResourceID != record.ResourceID || envelope.SecretGeneration != record.SecretGeneration || envelope.OperationID != record.OperationID {
			writeError(w, "one-time secret replay envelope scope mismatch", http.StatusForbidden)
			return
		}
		body = envelope.Body
	}
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
