package audit

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// fileSync is swapped in tests to exercise the file.Sync error path.
var fileSync = (*os.File).Sync

const (
	defaultRecorderMaxBytes int64 = 5 * 1024 * 1024
	defaultRecorderBackups        = 5
	redactedValue                 = "[REDACTED]"
)

type Record struct {
	Timestamp time.Time      `json:"timestamp"`
	Actor     string         `json:"actor"`
	Role      string         `json:"role,omitempty"`
	Action    string         `json:"action"`
	Target    string         `json:"target,omitempty"`
	IP        string         `json:"ip,omitempty"`
	UserAgent string         `json:"userAgent,omitempty"`
	RequestID string         `json:"requestId,omitempty"`
	Success   bool           `json:"success"`
	Error     string         `json:"error,omitempty"`
	Details   map[string]any `json:"details,omitempty"`
}

type RecorderOptions struct {
	MaxBytes int64
	Backups  int
	Now      func() time.Time
}

type Recorder struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	backups  int
	now      func() time.Time
}

func NewRecorder(path string, options RecorderOptions) *Recorder {
	maxBytes := options.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultRecorderMaxBytes
	}
	backups := options.Backups
	if backups == 0 {
		backups = defaultRecorderBackups
	}
	if backups < 0 {
		backups = 0
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Recorder{
		path:     path,
		maxBytes: maxBytes,
		backups:  backups,
		now:      now,
	}
}

func (r *Recorder) Append(record Record) error {
	if r == nil || r.path == "" {
		return nil
	}
	if record.Timestamp.IsZero() {
		record.Timestamp = r.now().UTC()
	} else {
		record.Timestamp = record.Timestamp.UTC()
	}
	record.Details = redactDetails(record.Details)
	body, err := json.Marshal(record)
	if err != nil {
		return err
	}
	body = append(body, '\n')

	r.mu.Lock()
	defer r.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(r.path), 0o700); err != nil {
		return err
	}
	if err := r.rotateIfNeededLocked(int64(len(body))); err != nil {
		return err
	}
	_, statErr := os.Stat(r.path)
	created := errors.Is(statErr, os.ErrNotExist)
	file, err := os.OpenFile(r.path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(body); err != nil {
		_ = file.Close()
		return err
	}
	if err := fileSync(file); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if created {
		return syncDirectory(filepath.Dir(r.path))
	}
	return nil
}

func (r *Recorder) List(limit int, before time.Time) ([]Record, error) {
	if r == nil || r.path == "" {
		return []Record{}, nil
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	records := make([]Record, 0, limit)
	for generation := 0; generation <= r.backups && len(records) < limit; generation++ {
		path := r.path
		if generation > 0 {
			path += "." + strconv.Itoa(generation)
		}
		fileRecords, err := readRecordsBounded(path, r.maxBytes)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for i := len(fileRecords) - 1; i >= 0 && len(records) < limit; i-- {
			record := fileRecords[i]
			if !before.IsZero() && !record.Timestamp.Before(before) {
				continue
			}
			records = append(records, record)
		}
	}
	sort.SliceStable(records, func(i, j int) bool {
		return records[i].Timestamp.After(records[j].Timestamp)
	})
	if len(records) > limit {
		records = records[:limit]
	}
	return records, nil
}

func (r *Recorder) rotateIfNeededLocked(incoming int64) error {
	info, err := os.Stat(r.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Size() == 0 || info.Size()+incoming <= r.maxBytes {
		return nil
	}
	if r.backups == 0 {
		return os.Remove(r.path)
	}
	oldest := r.path + "." + strconv.Itoa(r.backups)
	if err := os.Remove(oldest); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for generation := r.backups - 1; generation >= 1; generation-- {
		source := r.path + "." + strconv.Itoa(generation)
		target := r.path + "." + strconv.Itoa(generation+1)
		if _, err := os.Stat(source); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.Rename(source, target); err != nil {
			return err
		}
	}
	first := r.path + ".1"
	if err := os.Remove(first); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(r.path, first); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(r.path))
}

func readRecords(path string) ([]Record, error) {
	return readRecordsBounded(path, defaultRecorderMaxBytes)
}

func readRecordsBounded(path string, maxBytes int64) ([]Record, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxBytes {
		return nil, fmt.Errorf("audit log %s exceeds configured size limit", path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	records := make([]Record, 0)
	lines := bytes.Split(body, []byte{'\n'})
	for index, raw := range lines {
		line := index + 1
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		var record Record
		if err := json.Unmarshal(raw, &record); err != nil {
			if index == len(lines)-1 && !bytes.HasSuffix(body, []byte{'\n'}) {
				break
			}
			return nil, fmt.Errorf("%s:%d: %w", path, line, err)
		}
		records = append(records, record)
	}
	return records, nil
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func redactDetails(details map[string]any) map[string]any {
	if details == nil {
		return nil
	}
	redacted := make(map[string]any, len(details))
	for key, value := range details {
		if sensitiveAuditKey(key) {
			redacted[key] = redactedValue
			continue
		}
		redacted[key] = redactAuditValue(value)
	}
	return redacted
}

func redactAuditValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return redactDetails(typed)
	case []any:
		result := make([]any, len(typed))
		for i, item := range typed {
			result[i] = redactAuditValue(item)
		}
		return result
	default:
		return value
	}
}

func sensitiveAuditKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "-", ""), "_", ""))
	for _, marker := range []string{
		"password",
		"passwd",
		"token",
		"secret",
		"cookie",
		"csrf",
		"authorization",
		"privatekey",
		"apikey",
	} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}
