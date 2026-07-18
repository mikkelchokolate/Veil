package client

import (
	"database/sql"
	"fmt"
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

// TrafficStore persists byte counters and bucketed samples. Counters are
// absolute (client totals); samples are per-bucket deltas for history.
type TrafficStore struct{ db *sql.DB }

func NewTrafficStore(db *sql.DB) *TrafficStore { return &TrafficStore{db: db} }

// RecordSample attributes a sample to its binding/client, bumping the
// absolute counter and writing a bucketed delta. For monotonic samples the
// delta is computed against the provider's last raw reading.
func (s *TrafficStore) RecordSample(sm Sample) error {
	clientID := sm.ClientID
	if clientID == "" {
		var cid string
		err := s.db.QueryRow(`SELECT client_id FROM client_bindings WHERE id=?`, sm.BindingID).Scan(&cid)
		if err != nil {
			return fmt.Errorf("client: traffic resolve binding: %w", err)
		}
		clientID = cid
	}
	upDelta, downDelta := sm.UploadBytes, sm.DownloadBytes
	if sm.Monotonic {
		var lastUp, lastDown int64
		var lastObserved int64
		row := s.db.QueryRow(`SELECT last_upload_raw, last_download_raw, last_observed_at FROM traffic_runtime_state WHERE provider_key=?`, sm.ProviderKey)
		scanErr := row.Scan(&lastUp, &lastDown, &lastObserved)
		if scanErr == sql.ErrNoRows {
			// First observation of this provider: establish baseline, no delta.
			upDelta, downDelta = 0, 0
		} else {
			upDelta = sm.UploadBytes - lastUp
			downDelta = sm.DownloadBytes - lastDown
			if upDelta < 0 || downDelta < 0 {
				// Provider counter reset (runtime restart): treat this reading as
				// a fresh baseline, no negative delta.
				upDelta = 0
				if downDelta < 0 {
					downDelta = 0
				}
			}
		}
		_, _ = s.db.Exec(`INSERT INTO traffic_runtime_state (provider_key, last_upload_raw, last_download_raw, last_observed_at)
		  VALUES(?,?,?,?)
		  ON CONFLICT(provider_key) DO UPDATE SET last_upload_raw=excluded.last_upload_raw,
		    last_download_raw=excluded.last_download_raw, last_observed_at=excluded.last_observed_at`,
			sm.ProviderKey, sm.UploadBytes, sm.DownloadBytes, sm.AtUnix)
	}
	// Absolute counter.
	_, err := s.db.Exec(`INSERT INTO traffic_counters (client_id, binding_id, upload_bytes, download_bytes, updated_at)
	  VALUES(?,?,?,?,?)
	  ON CONFLICT(client_id, binding_id) DO UPDATE SET
	    upload_bytes=upload_bytes+excluded.upload_bytes,
	    download_bytes=download_bytes+excluded.download_bytes,
	    updated_at=excluded.updated_at`,
		clientID, sm.BindingID, upDelta, downDelta, sm.AtUnix)
	if err != nil {
		return fmt.Errorf("client: traffic counter: %w", err)
	}
	// Bucketed sample (bucket = truncated to minute).
	bucket := sm.AtUnix - (sm.AtUnix % 60)
	_, err = s.db.Exec(`INSERT INTO traffic_samples (bucket_start, client_id, binding_id, upload_delta, download_delta)
	  VALUES(?,?,?,?,?)
	  ON CONFLICT(bucket_start, client_id, binding_id) DO UPDATE SET
	    upload_delta=upload_delta+excluded.upload_delta,
	    download_delta=download_delta+excluded.download_delta`,
		bucket, clientID, sm.BindingID, upDelta, downDelta)
	if err != nil {
		return fmt.Errorf("client: traffic sample: %w", err)
	}
	return nil
}

// TotalsForClient returns absolute upload/download byte totals for a client.
func (s *TrafficStore) TotalsForClient(clientID string) (upload, download int64, err error) {
	row := s.db.QueryRow(`SELECT COALESCE(SUM(upload_bytes),0), COALESCE(SUM(download_bytes),0)
	  FROM traffic_counters WHERE client_id=?`, clientID)
	if err := row.Scan(&upload, &download); err != nil {
		return 0, 0, fmt.Errorf("client: traffic totals: %w", err)
	}
	return upload, download, nil
}

// ResetForClient zeroes a client's cumulative counters and bucketed samples.
// Called by the reconciler when a quota reset window rolls over so the new
// period starts from a clean slate.
func (s *TrafficStore) ResetForClient(clientID string) error {
	if _, err := s.db.Exec(`DELETE FROM traffic_counters WHERE client_id=?`, clientID); err != nil {
		return fmt.Errorf("client: traffic reset counters: %w", err)
	}
	if _, err := s.db.Exec(`DELETE FROM traffic_samples WHERE client_id=?`, clientID); err != nil {
		return fmt.Errorf("client: traffic reset samples: %w", err)
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
