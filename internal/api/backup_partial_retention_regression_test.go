package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/privileged"
)

// TestBackupPruneResolvesOmittedRetentionToDocumentedDefaults locks #1460:
// docs/openapi.yaml documents default 7/4/12 for BackupPruneRequest, but the
// handler decoded an omitted field as 0 and forwarded the raw triple to the
// privileged helper, which reads 0 as "keep none of that bucket". A partial
// body such as {"daily":7} therefore pruned every weekly and monthly
// generation the documented defaults keep — locally and on the mirrored SFTP
// destination. An explicit 0 has to stay expressible ("keep no dailies").
func TestBackupPruneResolvesOmittedRetentionToDocumentedDefaults(t *testing.T) {
	cases := []struct {
		name string
		body string
		want [3]int
	}{
		{name: "all omitted keeps the documented defaults", body: `{}`, want: [3]int{7, 4, 12}},
		{name: "daily only takes the weekly and monthly defaults", body: `{"daily":7}`, want: [3]int{7, 4, 12}},
		{name: "weekly only takes the daily and monthly defaults", body: `{"weekly":2}`, want: [3]int{7, 2, 12}},
		{name: "explicit zero keeps none of that bucket", body: `{"daily":0}`, want: [3]int{0, 4, 12}},
		{name: "a full triple is forwarded verbatim", body: `{"daily":2,"weekly":1,"monthly":0}`, want: [3]int{2, 1, 0}},
		{name: "an explicit all-zero triple keeps the #1412 defaults contract", body: `{"daily":0,"weekly":0,"monthly":0}`, want: [3]int{7, 4, 12}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := newPanelBackupState(t)
			var calls []privileged.BackupRequest
			state.privileged = backupStubClient{backup: func(_ context.Context, request privileged.BackupRequest) (privileged.BackupResult, error) {
				calls = append(calls, request)
				return privileged.BackupResult{}, nil
			}}

			response := httptest.NewRecorder()
			state.handleBackupPrune(response, adminJSONRequest(http.MethodPost, "/api/backups/prune", tc.body))
			if response.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if len(calls) != 1 || calls[0].Action != privileged.BackupActionPrune {
				t.Fatalf("helper calls = %+v, want one prune", calls)
			}
			got := [3]int{calls[0].Daily, calls[0].Weekly, calls[0].Monthly}
			if got != tc.want {
				t.Fatalf("body %s forwarded daily/weekly/monthly %v, want %v", tc.body, got, tc.want)
			}
		})
	}
}

// TestBackupCreatePruneResolvesOmittedRetentionToDocumentedDefaults covers the
// create+prune path of the same defect: POST /api/backups with "prune": true
// and a partial retention body applies the documented defaults to the fields
// the caller omitted.
func TestBackupCreatePruneResolvesOmittedRetentionToDocumentedDefaults(t *testing.T) {
	state := newPanelBackupState(t)
	var prune *privileged.BackupRequest
	state.privileged = backupStubClient{backup: func(_ context.Context, request privileged.BackupRequest) (privileged.BackupResult, error) {
		if request.Action == privileged.BackupActionPrune {
			call := request
			prune = &call
			return privileged.BackupResult{}, nil
		}
		return privileged.BackupResult{ArchiveName: "veil_backup_20240101_020000.tar.gz.enc", Verified: true}, nil
	}}

	response := httptest.NewRecorder()
	state.handleBackups(response, adminJSONRequest(http.MethodPost, "/api/backups", `{"prune":true,"daily":1}`))
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if prune == nil {
		t.Fatal("create with prune did not request a prune")
	}
	got := [3]int{prune.Daily, prune.Weekly, prune.Monthly}
	if want := [3]int{1, 4, 12}; got != want {
		t.Fatalf("create+prune forwarded daily/weekly/monthly %v, want %v", got, want)
	}
}

// TestBackupRetentionRejectsOutOfRangeOmittedDefaults keeps the range check
// on resolved values: the documented defaults pass, but an explicit count
// outside the API limits is still a 400.
func TestBackupRetentionRejectsOutOfRangeAfterResolution(t *testing.T) {
	state := newPanelBackupState(t)
	state.privileged = backupStubClient{backup: func(context.Context, privileged.BackupRequest) (privileged.BackupResult, error) {
		t.Fatal("helper must not be called for an out-of-range retention count")
		return privileged.BackupResult{}, nil
	}}
	response := httptest.NewRecorder()
	state.handleBackupPrune(response, adminJSONRequest(http.MethodPost, "/api/backups/prune", `{"weekly":105}`))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
