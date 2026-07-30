package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
)

type durableIdempotencyRecord struct {
	Fingerprint string
	State       string
	Owner       string
	Status      int
	Headers     []byte
	Body        []byte
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
		replayDurableIdempotencyResponse(w, record)
		return
	}

	capture := newBufferedResponse(w.Header())
	next.ServeHTTP(capture, r)
	status := capture.status
	if status == 0 {
		status = http.StatusOK
	}
	if status >= http.StatusOK && status < http.StatusBadRequest && capture.body.Len() <= maxIdempotencyBody {
		if err := s.completeDurable(scope, fingerprint, status, capture.header, capture.body.Bytes()); err != nil {
			writeError(w, "failed to finalize idempotent response", http.StatusServiceUnavailable)
			return
		}
	} else {
		_ = s.abortDurable(scope, fingerprint)
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
	result, err := s.db.Exec(`INSERT OR IGNORE INTO idempotency_records
  (scope, actor_id, endpoint, idempotency_key, payload_hash, state, owner_process,
   reserved_until, created_at, updated_at, expires_at)
  VALUES(?,?,?,?,?,'reserved',?,?,?,?,?)`,
		scope, actor, endpoint, key, fingerprint, s.owner,
		now.Add(2*time.Minute).Unix(), nowUnix, nowUnix, now.Add(idempotencyTTL).Unix())
	if err != nil {
		return false, durableIdempotencyRecord{}, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, durableIdempotencyRecord{}, err
	}
	if rows == 1 {
		return true, durableIdempotencyRecord{Fingerprint: fingerprint, State: "reserved", Owner: s.owner}, nil
	}
	record, err := s.readDurable(scope)
	return false, record, err
}

func (s *idempotencyStore) readDurable(scope string) (durableIdempotencyRecord, error) {
	var record durableIdempotencyRecord
	err := s.db.QueryRow(`SELECT payload_hash, state, owner_process,
  COALESCE(response_status,0), COALESCE(response_headers,X''), COALESCE(response_body,X'')
  FROM idempotency_records WHERE scope=?`, scope).
		Scan(&record.Fingerprint, &record.State, &record.Owner, &record.Status, &record.Headers, &record.Body)
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

func (s *idempotencyStore) completeDurable(scope, fingerprint string, status int, headers http.Header, body []byte) error {
	storedHeaders := headers.Clone()
	storedHeaders.Del("X-Request-ID")
	storedHeaders.Del("Set-Cookie")
	encodedHeaders, err := json.Marshal(storedHeaders)
	if err != nil {
		return err
	}
	now := s.now().UTC()
	result, err := s.db.Exec(`UPDATE idempotency_records SET
  state='completed', response_status=?, response_headers=?, response_body=?,
  updated_at=?, expires_at=?
  WHERE scope=? AND payload_hash=? AND owner_process=? AND state='reserved'`,
		status, encodedHeaders, append([]byte(nil), body...), now.Unix(), now.Add(idempotencyTTL).Unix(),
		scope, fingerprint, s.owner)
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
	return nil
}

func (s *idempotencyStore) abortDurable(scope, fingerprint string) error {
	_, err := s.db.Exec(`DELETE FROM idempotency_records
  WHERE scope=? AND payload_hash=? AND owner_process=? AND state='reserved'`, scope, fingerprint, s.owner)
	return err
}

func replayDurableIdempotencyResponse(w http.ResponseWriter, record durableIdempotencyRecord) {
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
	w.WriteHeader(status)
	_, _ = w.Write(record.Body)
}
