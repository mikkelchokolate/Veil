package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/privileged"
)

// Regression for #963: a download must share the backup mutation fence with
// prune/delete/restore — a mutation in progress rejects the download, and a
// download in flight rejects the mutation — instead of racing the archive.
func TestBackupDownloadIsFencedAgainstBackupMutation(t *testing.T) {
	state := newPanelBackupState(t)

	// A running mutation excludes downloads: the archive could be deleted or
	// replaced mid-transfer.
	state.backupMutationMu.Lock()
	downloadResponse := httptest.NewRecorder()
	state.handleBackupByName(downloadResponse, adminJSONRequest(http.MethodGet, "/api/backups/arc.enc/download", ""))
	state.backupMutationMu.Unlock()
	if downloadResponse.Code != http.StatusConflict {
		t.Fatalf("download during mutation status=%d body=%s, want 409", downloadResponse.Code, downloadResponse.Body.String())
	}

	// A download in flight excludes mutations: prune must not remove the
	// archive underneath the stream.
	state.backupMutationMu.RLock()
	pruneResponse := httptest.NewRecorder()
	state.handleBackupPrune(pruneResponse, adminJSONRequest(http.MethodPost, "/api/backups/prune", `{"daily":1,"weekly":0,"monthly":0}`))
	state.backupMutationMu.RUnlock()
	if pruneResponse.Code != http.StatusConflict {
		t.Fatalf("prune during download status=%d body=%s, want 409", pruneResponse.Code, pruneResponse.Body.String())
	}
}

// Regression for #963: once Content-Length has been committed, a mid-stream
// identity change (the archive was replaced behind the bound stream) must fail
// the transfer — the connection is aborted so the client gets an unambiguous
// truncation error rather than a short body that looks like a complete 200.
func TestBackupDownloadAbortsConnectionOnStreamIdentityDrift(t *testing.T) {
	state := newPanelBackupState(t)
	body := []byte(strings.Repeat("chunked-payload-", 150000)) // >2 MiB → 3 chunks
	digestBytes := sha256.Sum256(body)
	contentDigest := hex.EncodeToString(digestBytes[:])
	const transactionID = "0123456789abcdef0123456789abcdef"
	readCalls := 0
	state.privileged = backupStubClient{backup: func(_ context.Context, request privileged.BackupRequest) (privileged.BackupResult, error) {
		if request.Action != privileged.BackupActionRead {
			t.Fatalf("unexpected backup action %q", request.Action)
		}
		readCalls++
		start, end := request.Offset, request.Offset+request.Limit
		if end > int64(len(body)) {
			end = int64(len(body))
		}
		result := privileged.BackupResult{
			Archives: []privileged.BackupArchive{{Name: "drift.enc", Size: int64(len(body)), CreatedAt: "2026-08-01T00:00:00Z"}},
			Data:     body[start:end], More: end < int64(len(body)),
			TransactionID: transactionID, ContentDigest: contentDigest,
			InodeGeneration: "1:2:3", BoundSize: int64(len(body)),
		}
		if readCalls == 2 {
			// The same-named archive was replaced mid-stream: the digest no
			// longer matches the identity the download was bound to.
			result.ContentDigest = strings.Repeat("f", 64)
		}
		return result, nil
	}}

	response := httptest.NewRecorder()
	func() {
		defer func() {
			if recovered := recover(); recovered != http.ErrAbortHandler {
				t.Fatalf("identity drift must abort the connection, recovered=%v", recovered)
			}
		}()
		state.handleBackupByName(response, adminJSONRequest(http.MethodGet, "/api/backups/drift.enc/download", ""))
	}()
	if readCalls != 2 {
		t.Fatalf("helper reads=%d, want drift detected on the second chunk", readCalls)
	}
	// Only the first (pre-drift) chunk reached the body; the stream did not
	// silently complete short.
	if response.Body.Len() != 1024*1024 {
		t.Fatalf("body=%d bytes, want exactly the first committed 1 MiB chunk", response.Body.Len())
	}
	// The panic must still release the download read fence.
	if !state.backupMutationMu.TryLock() {
		t.Fatal("aborted download left the backup mutation fence held")
	}
	state.backupMutationMu.Unlock()
}

// Regression for #965: when a prune fails mid-operation, the error response
// still reports which archives were already deleted and which were classified
// as kept — the operator must not be left guessing about on-disk state.
func TestBackupPruneErrorReportsPartialResult(t *testing.T) {
	state := newPanelBackupState(t)
	state.privileged = backupStubClient{backup: func(_ context.Context, request privileged.BackupRequest) (privileged.BackupResult, error) {
		if request.Action != privileged.BackupActionPrune {
			t.Fatalf("unexpected backup action %q", request.Action)
		}
		return privileged.BackupResult{
			Pruned: []string{"veil_backup_20260301_020000.tar.gz.enc"},
			Kept:   []string{"veil_backup_20260401_020000.tar.gz.enc"},
		}, errors.New("remove backup archive veil_backup_20260201_020000.tar.gz.enc: injected")
	}}

	response := httptest.NewRecorder()
	state.handleBackupPrune(response, adminJSONRequest(http.MethodPost, "/api/backups/prune", `{"daily":1,"weekly":0,"monthly":0}`))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("prune failure status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Deleted []string `json:"deleted"`
		Kept    []string `json:"kept"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode prune error response: %v", err)
	}
	if body.Error.Code == "" {
		t.Fatalf("error envelope missing code: %s", response.Body.String())
	}
	if len(body.Deleted) != 1 || body.Deleted[0] != "veil_backup_20260301_020000.tar.gz.enc" {
		t.Fatalf("partial prune deleted=%v, want the already-removed archive", body.Deleted)
	}
	if len(body.Kept) != 1 || body.Kept[0] != "veil_backup_20260401_020000.tar.gz.enc" {
		t.Fatalf("partial prune kept=%v, want the retained archive", body.Kept)
	}
}
