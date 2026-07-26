package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/backup"
	"github.com/mikkelchokolate/Veil/internal/privileged"
)

func TestBackupCreateListAndVerifyReturnStableMetadata(t *testing.T) {
	state := newPanelBackupState(t)
	create := adminJSONRequest(http.MethodPost, "/api/backups", `{}`)
	createResponse := httptest.NewRecorder()
	state.handleBackups(createResponse, create)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createResponse.Code, createResponse.Body.String())
	}
	var created BackupCreateResponse
	if err := json.NewDecoder(createResponse.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.Archive.Name == "" || created.Archive.Path != filepath.Join(state.backupDir, created.Archive.Name) {
		t.Fatalf("archive identity = %+v", created.Archive)
	}
	if created.Archive.Size <= 0 || created.Archive.CreatedAt.IsZero() || !created.Archive.Encrypted {
		t.Fatalf("archive metadata = %+v", created.Archive)
	}
	if !created.Verification.Encrypted {
		t.Fatalf("create verification = %+v", created.Verification)
	}

	verify := adminJSONRequest(http.MethodPost, "/api/backups/"+created.Archive.Name+"/verify", `{}`)
	verifyResponse := httptest.NewRecorder()
	state.handleBackupByName(verifyResponse, verify)
	if verifyResponse.Code != http.StatusOK {
		t.Fatalf("verify status=%d body=%s", verifyResponse.Code, verifyResponse.Body.String())
	}
	var report backup.VerificationReport
	if err := json.NewDecoder(verifyResponse.Body).Decode(&report); err != nil {
		t.Fatal(err)
	}
	if !report.Encrypted {
		t.Fatalf("verify report = %+v", report)
	}

	list := adminJSONRequest(http.MethodGet, "/api/backups", "")
	listResponse := httptest.NewRecorder()
	state.handleBackups(listResponse, list)
	var archives []backup.ArchiveEntry
	if err := json.NewDecoder(listResponse.Body).Decode(&archives); err != nil {
		t.Fatal(err)
	}
	if len(archives) != 1 || archives[0].Path != created.Archive.Path || archives[0].Size != created.Archive.Size {
		t.Fatalf("listed archives = %+v, created = %+v", archives, created.Archive)
	}
}

func TestBackupCreateReturnsCreatedWhenOptionalPruneFails(t *testing.T) {
	state := newPanelBackupState(t)
	createdAt := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	createCalls := 0
	pruneCalls := 0
	state.privileged = backupStubClient{backup: func(_ context.Context, request privileged.BackupRequest) (privileged.BackupResult, error) {
		switch request.Action {
		case privileged.BackupActionCreate:
			createCalls++
			return privileged.BackupResult{
				ArchiveName: "veil_backup_20260711_120000.tar.gz.enc",
				Archives: []privileged.BackupArchive{{
					Name: "veil_backup_20260711_120000.tar.gz.enc", Size: 1234,
					CreatedAt: createdAt.Format(time.RFC3339), Encrypted: true,
				}},
				Verified: true,
				Verification: &privileged.BackupVerificationReport{
					FormatVersion: 1, EncryptionVersion: 2, Encrypted: true,
					CreatedAt: createdAt.Format(time.RFC3339), VeilVersion: "test",
					StateSchemaVersion: 1,
					Files: []privileged.BackupVerificationFile{
						{Name: "state.json", Size: 10, SHA256: strings.Repeat("a", 64)},
						{Name: "state.key", Size: 32, SHA256: strings.Repeat("b", 64)},
					},
				},
			}, nil
		case privileged.BackupActionPrune:
			pruneCalls++
			return privileged.BackupResult{}, &privileged.Error{
				Code: privileged.ErrorOperationFailed, Message: "simulated prune failure",
			}
		default:
			t.Fatalf("unexpected backup action %q", request.Action)
			return privileged.BackupResult{}, nil
		}
	}}

	request := adminJSONRequest(http.MethodPost, "/api/backups", `{"prune":true,"daily":7,"weekly":4,"monthly":12}`)
	response := httptest.NewRecorder()
	state.handleBackups(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var result BackupCreateResponse
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Archive.Name == "" || result.Archive.Size != 1234 {
		t.Fatalf("created archive metadata = %+v", result.Archive)
	}
	assertCompleteBackupVerification(t, result.Verification)
	if !strings.Contains(result.Warning, "backup created") || !strings.Contains(result.Warning, "simulated prune failure") {
		t.Fatalf("warning = %q", result.Warning)
	}
	if result.Prune != nil {
		t.Fatalf("failed prune unexpectedly returned a result: %+v", result.Prune)
	}
	if createCalls != 1 || pruneCalls != 1 {
		t.Fatalf("create calls=%d prune calls=%d", createCalls, pruneCalls)
	}
}

func TestBackupDownloadStreamsHelperChunks(t *testing.T) {
	state := newPanelBackupState(t)
	body := []byte(strings.Repeat("chunked-download-payload-", 100000))
	readCalls := 0
	state.privileged = backupStubClient{backup: func(_ context.Context, request privileged.BackupRequest) (privileged.BackupResult, error) {
		if request.Action != privileged.BackupActionRead {
			t.Fatalf("unexpected backup action %q", request.Action)
		}
		readCalls++
		if request.Limit <= 0 || request.Limit > 1024*1024 {
			t.Fatalf("invalid bounded read limit %d", request.Limit)
		}
		start := request.Offset
		end := start + request.Limit
		if end > int64(len(body)) {
			end = int64(len(body))
		}
		return privileged.BackupResult{
			Data: body[start:end],
			More: end < int64(len(body)),
		}, nil
	}}

	request := adminJSONRequest(http.MethodGet, "/api/backups/large.enc/download", "")
	response := httptest.NewRecorder()
	state.handleBackupByName(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if response.Body.String() != string(body) {
		t.Fatalf("download bytes=%d want=%d", response.Body.Len(), len(body))
	}
	if readCalls < 2 {
		t.Fatalf("helper read calls=%d, want multiple bounded chunks", readCalls)
	}
}

func assertCompleteBackupVerification(t *testing.T, report backup.VerificationReport) {
	t.Helper()
	if report.FormatVersion <= 0 || report.EncryptionVersion <= 0 || !report.Encrypted || report.Legacy {
		t.Fatalf("verification header = %+v", report)
	}
	if report.CreatedAt.IsZero() || report.VeilVersion == "" || report.StateSchemaVersion <= 0 {
		t.Fatalf("verification metadata = %+v", report)
	}
	if len(report.Files) != 2 {
		t.Fatalf("verification files = %+v", report.Files)
	}
	for _, file := range report.Files {
		if file.Name == "" || file.Size <= 0 || len(file.SHA256) != 64 {
			t.Fatalf("verification file = %+v", file)
		}
	}
}

type backupStubClient struct {
	privileged.Client
	backup func(context.Context, privileged.BackupRequest) (privileged.BackupResult, error)
}

func (client backupStubClient) Backup(ctx context.Context, request privileged.BackupRequest) (privileged.BackupResult, error) {
	return client.backup(ctx, request)
}
