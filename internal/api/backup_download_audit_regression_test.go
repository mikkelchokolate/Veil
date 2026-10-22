package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/audit"
	"github.com/mikkelchokolate/Veil/internal/privileged"
)

// downloadAuditRecords returns every backup.download record in the panel
// audit log.
func downloadAuditRecords(t *testing.T, state *managementState) []audit.Record {
	t.Helper()
	records, err := state.auditRecorder().List(100, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var downloads []audit.Record
	for _, record := range records {
		if record.Action == "backup.download" {
			downloads = append(downloads, record)
		}
	}
	return downloads
}

// singleDownloadAudit asserts that exactly one backup.download record exists
// and returns it.
func singleDownloadAudit(t *testing.T, state *managementState) audit.Record {
	t.Helper()
	records := downloadAuditRecords(t, state)
	if len(records) != 1 {
		t.Fatalf("backup.download audit records=%d (%+v), want exactly one per attempt", len(records), records)
	}
	return records[0]
}

func auditedBytes(t *testing.T, record audit.Record) int64 {
	t.Helper()
	switch value := record.Details["bytes"].(type) {
	case int64:
		return value
	case float64:
		return int64(value)
	case int:
		return int64(value)
	default:
		t.Fatalf("backup.download record has no numeric bytes detail: %+v", record.Details)
		return 0
	}
}

// chunkedArchiveStub serves body in bounded helper chunks under one stable
// stream identity; mutate (optional) may rewrite the result of read call n.
func chunkedArchiveStub(t *testing.T, name string, body []byte, mutate func(call int, result *privileged.BackupResult)) backupStubClient {
	t.Helper()
	digest := sha256.Sum256(body)
	calls := 0
	return backupStubClient{backup: func(_ context.Context, request privileged.BackupRequest) (privileged.BackupResult, error) {
		if request.Action != privileged.BackupActionRead {
			t.Fatalf("unexpected backup action %q", request.Action)
		}
		calls++
		end := request.Offset + request.Limit
		if end > int64(len(body)) {
			end = int64(len(body))
		}
		result := privileged.BackupResult{
			Archives:        []privileged.BackupArchive{{Name: name, Size: int64(len(body)), CreatedAt: "2026-08-01T00:00:00Z"}},
			Data:            body[request.Offset:end],
			More:            end < int64(len(body)),
			TransactionID:   "0123456789abcdef0123456789abcdef",
			ContentDigest:   hex.EncodeToString(digest[:]),
			InodeGeneration: "1:2:3",
			BoundSize:       int64(len(body)),
		}
		if mutate != nil {
			mutate(calls, &result)
		}
		return result, nil
	}}
}

// Regression for #1268 item 1: the archive download exfiltrates the complete
// panel state, yet it was the only backup operation that left no audit trail.
// A completed download must record who fetched which archive and how much.
func TestBackupDownloadRecordsSuccessfulAudit(t *testing.T) {
	state := newPanelBackupState(t)
	body := []byte(strings.Repeat("audited-download-", 100000)) // >1 MiB → 2 chunks
	state.privileged = chunkedArchiveStub(t, "audited.enc", body, nil)

	response := httptest.NewRecorder()
	state.handleBackupByName(response, adminJSONRequest(http.MethodGet, "/api/backups/audited.enc/download", ""))
	if response.Code != http.StatusOK || response.Body.Len() != len(body) {
		t.Fatalf("download status=%d bytes=%d, want 200 with %d bytes", response.Code, response.Body.Len(), len(body))
	}
	record := singleDownloadAudit(t, state)
	if !record.Success || record.Error != "" {
		t.Fatalf("completed download audited as failure: %+v", record)
	}
	if record.Target != "audited.enc" {
		t.Fatalf("audit target=%q, want the archive name", record.Target)
	}
	if record.Actor == "" || record.Actor == "system" {
		t.Fatalf("audit actor=%q, want the requesting admin", record.Actor)
	}
	if got := auditedBytes(t, record); got != int64(len(body)) {
		t.Fatalf("audited bytes=%d, want %d", got, len(body))
	}
}

// A drift abort panics with http.ErrAbortHandler after headers were
// committed; the audit record must still be written and must not claim
// success for the truncated transfer.
func TestBackupDownloadAuditsIdentityDriftAbort(t *testing.T) {
	state := newPanelBackupState(t)
	body := []byte(strings.Repeat("drift-audit-", 250000)) // >2 MiB → 3 chunks
	state.privileged = chunkedArchiveStub(t, "drift.enc", body, func(call int, result *privileged.BackupResult) {
		if call == 2 {
			result.ContentDigest = strings.Repeat("f", 64)
		}
	})

	func() {
		defer func() {
			if recovered := recover(); recovered != http.ErrAbortHandler {
				t.Fatalf("identity drift must abort the connection, recovered=%v", recovered)
			}
		}()
		state.handleBackupByName(httptest.NewRecorder(), adminJSONRequest(http.MethodGet, "/api/backups/drift.enc/download", ""))
	}()
	record := singleDownloadAudit(t, state)
	if record.Success || !strings.Contains(record.Error, "drifted") {
		t.Fatalf("drift abort audit=%+v, want a failure naming the identity drift", record)
	}
	if got := auditedBytes(t, record); got != 1024*1024 {
		t.Fatalf("audited bytes=%d, want the one committed 1 MiB chunk", got)
	}
}

// The client walking away mid-stream is not a completed exfiltration but
// still leaves a record with the bytes that were already sent.
func TestBackupDownloadAuditsClientDisconnect(t *testing.T) {
	state := newPanelBackupState(t)
	body := []byte(strings.Repeat("disconnect-audit-", 150000))
	state.privileged = chunkedArchiveStub(t, "gone.enc", body, nil)

	writer := &failingDownloadWriter{header: make(http.Header)}
	state.handleBackupByName(writer, adminJSONRequest(http.MethodGet, "/api/backups/gone.enc/download", ""))
	record := singleDownloadAudit(t, state)
	if record.Success || !strings.Contains(record.Error, "client connection failed") {
		t.Fatalf("disconnect audit=%+v, want a client-connection failure", record)
	}
	if got := auditedBytes(t, record); got != 0 {
		t.Fatalf("audited bytes=%d, want 0 (the first write failed)", got)
	}
}

// Refused attempts are audited too: a helper failure before any byte is
// sent, and a download rejected by the backup mutation fence.
func TestBackupDownloadAuditsRefusedAttempts(t *testing.T) {
	t.Run("helper error", func(t *testing.T) {
		state := newPanelBackupState(t)
		state.privileged = backupStubClient{backup: func(context.Context, privileged.BackupRequest) (privileged.BackupResult, error) {
			return privileged.BackupResult{}, errors.New("archive not found: injected")
		}}
		response := httptest.NewRecorder()
		state.handleBackupByName(response, adminJSONRequest(http.MethodGet, "/api/backups/missing.enc/download", ""))
		if response.Code == http.StatusOK {
			t.Fatalf("helper failure answered 200: %s", response.Body.String())
		}
		record := singleDownloadAudit(t, state)
		if record.Success || !strings.Contains(record.Error, "injected") {
			t.Fatalf("helper failure audit=%+v, want the helper error", record)
		}
	})
	t.Run("mutation fence held", func(t *testing.T) {
		state := newPanelBackupState(t)
		state.backupMutationMu.Lock()
		response := httptest.NewRecorder()
		state.handleBackupByName(response, adminJSONRequest(http.MethodGet, "/api/backups/arc.enc/download", ""))
		state.backupMutationMu.Unlock()
		if response.Code != http.StatusConflict {
			t.Fatalf("download during mutation status=%d, want 409", response.Code)
		}
		record := singleDownloadAudit(t, state)
		if record.Success || !strings.Contains(record.Error, "already in progress") {
			t.Fatalf("fenced download audit=%+v, want the fence conflict", record)
		}
	})
	t.Run("wrong method is not a download attempt", func(t *testing.T) {
		state := newPanelBackupState(t)
		response := httptest.NewRecorder()
		state.handleBackupByName(response, adminJSONRequest(http.MethodPost, "/api/backups/arc.enc/download", ""))
		if response.Code != http.StatusMethodNotAllowed {
			t.Fatalf("POST download status=%d, want 405", response.Code)
		}
		if records := downloadAuditRecords(t, state); len(records) != 0 {
			t.Fatalf("405 left backup.download records: %+v", records)
		}
	})
}
