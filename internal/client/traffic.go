package client

import (
	"database/sql"
	"fmt"
	"math"
	"regexp"
	"sync"
	"time"
)

// Sample is a single traffic observation. Deltas (non-monotonic) are summed;
// monotonic readings carry an absolute provider counter and are stored via
// runtime state so successive readings diff without double-counting.
type Sample struct {
	BindingID     string
	UploadBytes   int64
	DownloadBytes int64
	AtUnix        int64
	Monotonic     bool   // when true, values are absolute provider counters
	ProviderKey   string // identifies the monotonic source (for runtime state)
	ClientID      string // denormalized attribution; resolved when empty
}

// TrafficStore persists current-quota-period counters and lifetime bucketed
// samples. A quota rollover resets only counters; analytics history remains.
type TrafficStore struct {
	db       *sql.DB
	recordMu sync.Mutex
}

func NewTrafficStore(db *sql.DB) *TrafficStore { return &TrafficStore{db: db} }

var providerKeyPattern = regexp.MustCompile(`^[A-Za-z0-9:._-]{1,160}$`)

// WithRecordLock serializes quota rollover with sample recording. The callback
// may open its own SQLite transaction; it must not call RecordSample.
func (s *TrafficStore) WithRecordLock(fn func() error) error {
	s.recordMu.Lock()
	defer s.recordMu.Unlock()
	return fn()
}

func validateTrafficSample(sm Sample) error {
	if sm.UploadBytes < 0 || sm.DownloadBytes < 0 || sm.AtUnix < 0 || sm.AtUnix > time.Now().Add(5*time.Minute).Unix() {
		return fmt.Errorf("client: traffic sample counters and timestamp are outside valid bounds")
	}
	if sm.Monotonic && !providerKeyPattern.MatchString(sm.ProviderKey) {
		return fmt.Errorf("client: invalid traffic provider key")
	}
	return nil
}

// RecordSample attributes a sample to its binding/client, bumping the
// absolute counter and writing a bucketed delta. For monotonic samples the
// delta is computed against the provider's last raw reading.
func (s *TrafficStore) RecordSample(sm Sample) error {
	if err := validateTrafficSample(sm); err != nil {
		return err
	}
	s.recordMu.Lock()
	defer s.recordMu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("client: begin traffic sample transaction: %w", err)
	}
	defer tx.Rollback()

	if err := s.recordSampleTx(tx, sm); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("client: commit traffic sample transaction: %w", err)
	}
	return nil
}

func (s *TrafficStore) RecordSamples(samples []Sample) error {
	seenProviderKeys := make(map[string]struct{}, len(samples))
	for _, sample := range samples {
		if err := validateTrafficSample(sample); err != nil {
			return err
		}
		if sample.Monotonic {
			if _, exists := seenProviderKeys[sample.ProviderKey]; exists {
				return fmt.Errorf("client: duplicate traffic provider key in batch")
			}
			seenProviderKeys[sample.ProviderKey] = struct{}{}
		}
	}
	s.recordMu.Lock()
	defer s.recordMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, sample := range samples {
		if err := s.recordSampleTx(tx, sample); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *TrafficStore) recordSampleTx(tx *sql.Tx, sm Sample) error {
	clientID := sm.ClientID
	if sm.BindingID != "" {
		var owner string
		if err := tx.QueryRow(`SELECT client_id FROM client_bindings WHERE id=?`, sm.BindingID).Scan(&owner); err != nil {
			return fmt.Errorf("client: traffic resolve binding ownership: %w", err)
		}
		if clientID != "" && clientID != owner {
			return fmt.Errorf("client: traffic binding does not belong to client")
		}
		clientID = owner
	} else if clientID != "" {
		var exists int
		if err := tx.QueryRow(`SELECT 1 FROM clients WHERE id=?`, clientID).Scan(&exists); err != nil {
			return fmt.Errorf("client: traffic resolve client ownership: %w", err)
		}
	}
	if clientID == "" {
		var cid string
		if err := tx.QueryRow(`SELECT client_id FROM client_bindings WHERE id=?`, sm.BindingID).Scan(&cid); err != nil {
			return fmt.Errorf("client: traffic resolve binding: %w", err)
		}
		clientID = cid
	}
	upDelta, downDelta := sm.UploadBytes, sm.DownloadBytes
	if sm.Monotonic {
		var lastUp, lastDown int64
		var lastObserved int64
		row := tx.QueryRow(`SELECT last_upload_raw, last_download_raw, last_observed_at FROM traffic_runtime_state WHERE provider_key=?`, sm.ProviderKey)
		scanErr := row.Scan(&lastUp, &lastDown, &lastObserved)
		if scanErr == sql.ErrNoRows {
			// First observation of this provider: establish baseline, no delta.
			upDelta, downDelta = 0, 0
		} else if scanErr != nil {
			return fmt.Errorf("client: traffic runtime state: %w", scanErr)
		} else {
			if sm.AtUnix < lastObserved {
				return fmt.Errorf("client: stale traffic provider timestamp")
			}
			upDelta = sm.UploadBytes - lastUp
			downDelta = sm.DownloadBytes - lastDown
			if upDelta < 0 {
				upDelta = 0
			}
			if downDelta < 0 {
				downDelta = 0
			}
		}
		if _, err := tx.Exec(`INSERT INTO traffic_runtime_state (provider_key, last_upload_raw, last_download_raw, last_observed_at)
		  VALUES(?,?,?,?)
		  ON CONFLICT(provider_key) DO UPDATE SET last_upload_raw=excluded.last_upload_raw,
		    last_download_raw=excluded.last_download_raw, last_observed_at=excluded.last_observed_at`,
			sm.ProviderKey, sm.UploadBytes, sm.DownloadBytes, sm.AtUnix); err != nil {
			return fmt.Errorf("client: traffic runtime state update: %w", err)
		}
	}
	var currentUpload, currentDownload int64
	counterErr := tx.QueryRow(`SELECT upload_bytes, download_bytes FROM traffic_counters WHERE client_id=? AND binding_id=?`, clientID, sm.BindingID).Scan(&currentUpload, &currentDownload)
	if counterErr != nil && counterErr != sql.ErrNoRows {
		return fmt.Errorf("client: traffic counter read: %w", counterErr)
	}
	if currentUpload < 0 || currentDownload < 0 || upDelta > math.MaxInt64-currentUpload || downDelta > math.MaxInt64-currentDownload {
		return fmt.Errorf("client: traffic counter overflow")
	}
	// Absolute counter.
	if _, err := tx.Exec(`INSERT INTO traffic_counters (client_id, binding_id, upload_bytes, download_bytes, updated_at)
	  VALUES(?,?,?,?,?)
	  ON CONFLICT(client_id, binding_id) DO UPDATE SET
	    upload_bytes=upload_bytes+excluded.upload_bytes,
	    download_bytes=download_bytes+excluded.download_bytes,
	    updated_at=excluded.updated_at`,
		clientID, sm.BindingID, upDelta, downDelta, sm.AtUnix); err != nil {
		return fmt.Errorf("client: traffic counter: %w", err)
	}
	// Bucketed sample (bucket = truncated to minute).
	bucket := sm.AtUnix - (sm.AtUnix % 60)
	var bucketUpload, bucketDownload int64
	bucketErr := tx.QueryRow(`SELECT upload_delta, download_delta FROM traffic_samples WHERE bucket_start=? AND client_id=? AND binding_id=?`, bucket, clientID, sm.BindingID).Scan(&bucketUpload, &bucketDownload)
	if bucketErr != nil && bucketErr != sql.ErrNoRows {
		return fmt.Errorf("client: traffic sample read: %w", bucketErr)
	}
	if bucketUpload < 0 || bucketDownload < 0 || upDelta > math.MaxInt64-bucketUpload || downDelta > math.MaxInt64-bucketDownload {
		return fmt.Errorf("client: traffic sample overflow")
	}
	if _, err := tx.Exec(`INSERT INTO traffic_samples (bucket_start, client_id, binding_id, upload_delta, download_delta)
	  VALUES(?,?,?,?,?)
	  ON CONFLICT(bucket_start, client_id, binding_id) DO UPDATE SET
	    upload_delta=upload_delta+excluded.upload_delta,
	    download_delta=download_delta+excluded.download_delta`,
		bucket, clientID, sm.BindingID, upDelta, downDelta); err != nil {
		return fmt.Errorf("client: traffic sample: %w", err)
	}
	return nil
}

// AggregateTotals returns usage across every traffic counter without loading
// clients or issuing per-client queries.
func (s *TrafficStore) AggregateTotals() (upload, download int64, err error) {
	row := s.db.QueryRow(`SELECT COALESCE(SUM(upload_bytes),0), COALESCE(SUM(download_bytes),0) FROM traffic_counters`)
	if err := row.Scan(&upload, &download); err != nil {
		return 0, 0, fmt.Errorf("client: aggregate traffic totals: %w", err)
	}
	return upload, download, nil
}

// TotalsForClient returns current quota-period upload/download usage.
func (s *TrafficStore) TotalsForClient(clientID string) (upload, download int64, err error) {
	row := s.db.QueryRow(`SELECT COALESCE(SUM(upload_bytes),0), COALESCE(SUM(download_bytes),0)
	  FROM traffic_counters WHERE client_id=?`, clientID)
	if err := row.Scan(&upload, &download); err != nil {
		return 0, 0, fmt.Errorf("client: traffic totals: %w", err)
	}
	return upload, download, nil
}

// ResetForClient zeroes current-period usage while retaining lifetime samples.
func (s *TrafficStore) ResetForClient(clientID string) error {
	return s.WithRecordLock(func() error {
		tx, err := s.db.Begin()
		if err != nil {
			return fmt.Errorf("client: begin quota reset transaction: %w", err)
		}
		defer tx.Rollback()
		if err := ResetQuotaPeriodTx(tx, clientID); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("client: commit quota reset transaction: %w", err)
		}
		return nil
	})
}

// ResetQuotaPeriodTx joins a caller-managed transaction. It intentionally does
// not delete traffic_samples: those rows are lifetime analytics.
func ResetQuotaPeriodTx(q DBTX, clientID string) error {
	if _, err := q.Exec(`DELETE FROM traffic_counters WHERE client_id=?`, clientID); err != nil {
		return fmt.Errorf("client: traffic reset counters: %w", err)
	}
	return nil
}

// MonotonicTotals returns the latest absolute reading per provider (last
// reading wins, no summation). providers maps providerKey -> bindingIDs; only
// the latest runtime_state row per key contributes. This is a read helper for
// reconciler tests; the authoritative path is RecordSample's delta logic.
func (s *TrafficStore) MonotonicTotals(providers map[string][]string) (upload, download int64, err error) {
	for key := range providers {
		var up, down int64
		row := s.db.QueryRow(`SELECT last_upload_raw, last_download_raw FROM traffic_runtime_state WHERE provider_key=?`, key)
		if err := row.Scan(&up, &down); err != nil {
			continue
		}
		upload += up
		download += down
	}
	return upload, download, nil
}

// SampleRow is one bucketed history row.
type SampleRow struct {
	BucketStart   int64  `json:"bucketStart"`
	ClientID      string `json:"clientId"`
	BindingID     string `json:"bindingId"`
	UploadDelta   int64  `json:"uploadDelta"`
	DownloadDelta int64  `json:"downloadDelta"`
}

// HistoryForBinding returns bucketed deltas for a binding within [from,to].
func (s *TrafficStore) HistoryForBinding(bindingID string, from, to int64, limit int) ([]SampleRow, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.db.Query(`SELECT bucket_start, client_id, binding_id, upload_delta, download_delta
	  FROM traffic_samples WHERE binding_id=? AND bucket_start>=? AND bucket_start<=?
	  ORDER BY bucket_start ASC LIMIT ?`, bindingID, from, to, limit)
	if err != nil {
		return nil, fmt.Errorf("client: traffic history: %w", err)
	}
	defer rows.Close()
	var out []SampleRow
	for rows.Next() {
		var r SampleRow
		if err := rows.Scan(&r.BucketStart, &r.ClientID, &r.BindingID, &r.UploadDelta, &r.DownloadDelta); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// HistoryForClient aggregates bucketed deltas across a client's bindings.
func (s *TrafficStore) HistoryForClient(clientID string, from, to int64, limit int) ([]SampleRow, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.db.Query(`SELECT bucket_start, client_id, '', SUM(upload_delta), SUM(download_delta)
	  FROM traffic_samples WHERE client_id=? AND bucket_start>=? AND bucket_start<=?
	  GROUP BY bucket_start, client_id ORDER BY bucket_start ASC LIMIT ?`, clientID, from, to, limit)
	if err != nil {
		return nil, fmt.Errorf("client: traffic history: %w", err)
	}
	defer rows.Close()
	var out []SampleRow
	for rows.Next() {
		var r SampleRow
		if err := rows.Scan(&r.BucketStart, &r.ClientID, &r.BindingID, &r.UploadDelta, &r.DownloadDelta); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
