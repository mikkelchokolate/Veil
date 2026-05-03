package installer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// AuditEvent represents a single audit log entry for rollback operations.
type AuditEvent struct {
	Timestamp     string   `json:"timestamp"`
	Action        string   `json:"action"`
	BackupID      string   `json:"backupID"`
	Success       bool     `json:"success"`
	Error         string   `json:"error,omitempty"`
	RestoredFiles []string `json:"restoredFiles,omitempty"`
}

// AppendAuditEvent appends one compact JSON line to the audit log.
// If path is empty, it's a no-op. Creates parent directories with 0755,
// and the log file itself with mode 0600.
func AppendAuditEvent(path string, event AuditEvent) error {
	if path == "" {
		return nil
	}

	event.Timestamp = time.Now().UTC().Format(time.RFC3339)

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	data, err := json.Marshal(event)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.Write(append(data, '\n')); err != nil {
		return err
	}

	return nil
}
