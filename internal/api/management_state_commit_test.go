package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/managementstate"
)

func TestSettingsMutationRevisionFailureRestoresStateFileAndRestart(t *testing.T) {
	router, state := newApplyTrackedRouterWithState(t)
	commitRequest := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewBufferString(
		`{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"accepted-before-failure.example.com"}`,
	))
	commitRequest.Header.Set("Content-Type", "application/json")
	commitResponse := httptest.NewRecorder()
	router.ServeHTTP(commitResponse, commitRequest)
	if commitResponse.Code != http.StatusOK {
		t.Fatalf("seed committed revision status=%d body=%s", commitResponse.Code, commitResponse.Body.String())
	}
	statePath := state.statePath
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state before mutation: %v", err)
	}
	beforeDomain := state.settings.Domain
	revisionsBefore, err := state.applyRevisions.Get()
	if err != nil {
		t.Fatalf("read revisions before mutation: %v", err)
	}
	jobsBefore, err := state.applyJobs.List(100)
	if err != nil {
		t.Fatalf("list jobs before mutation: %v", err)
	}

	// Keep the table readable while forcing the desired-revision update inside
	// SaveLocked's SQLite transaction to abort.
	if _, err := state.db.Exec(`
CREATE TRIGGER sabotage_revisions_update
BEFORE UPDATE OF desired_revision ON revisions
BEGIN
  SELECT RAISE(ABORT, 'sabotaged revisions table');
END`); err != nil {
		t.Fatalf("sabotage revisions table: %v", err)
	}

	request := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewBufferString(
		`{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"rejected.example.com"}`,
	))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code < http.StatusBadRequest {
		t.Fatalf("settings unexpectedly succeeded with status %d: %s", response.Code, response.Body.String())
	}

	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state after failed mutation: %v", err)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("state file changed after failed mutation\nbefore=%s\nafter=%s", before, after)
	}
	if state.settings.Domain != beforeDomain {
		t.Fatalf("in-memory domain = %q, want unchanged %q", state.settings.Domain, beforeDomain)
	}
	revisionsAfter, err := state.applyRevisions.Get()
	if err != nil {
		t.Fatalf("read revisions after mutation: %v", err)
	}
	if revisionsAfter != revisionsBefore {
		t.Fatalf("revisions changed after failed mutation: before=%+v after=%+v", revisionsBefore, revisionsAfter)
	}
	if state.applySnapshots.Has(revisionsBefore.Desired + 1) {
		t.Fatalf("immutable snapshot exists for rejected revision %d", revisionsBefore.Desired+1)
	}
	jobsAfter, err := state.applyJobs.List(100)
	if err != nil {
		t.Fatalf("list jobs after mutation: %v", err)
	}
	if len(jobsAfter) != len(jobsBefore) {
		t.Fatalf("apply jobs changed after failed mutation: before=%d after=%d", len(jobsBefore), len(jobsAfter))
	}
	for _, path := range []string{statePath + ".pending-mutation.json", statePath + ".pending-mutation.previous"} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("failed mutation left recovery artifact at %s: %v", path, err)
		}
	}

	if err := closeClientSubsystem(state); err != nil {
		t.Fatalf("close panel database before restart: %v", err)
	}
	restarted := newManagementState(ServerInfo{
		Version:   "test",
		Mode:      "dev",
		StatePath: statePath,
		ApplyRoot: filepath.Dir(statePath),
	})
	defer closeClientSubsystem(restarted)
	if restarted.settings.Domain != beforeDomain {
		t.Fatalf("rejected domain became active after restart: got %q want %q", restarted.settings.Domain, beforeDomain)
	}
	restartedBytes, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state after restart: %v", err)
	}
	if !bytes.Equal(restartedBytes, before) {
		t.Fatalf("state file changed across restart after rejected mutation")
	}
}

func TestStartupRollsBackInterruptedStatePublicationBeforeSQLiteCommit(t *testing.T) {
	router, state := newApplyTrackedRouterWithState(t)
	commitRequest := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewBufferString(
		`{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"accepted-before-crash.example.com"}`,
	))
	commitRequest.Header.Set("Content-Type", "application/json")
	commitResponse := httptest.NewRecorder()
	router.ServeHTTP(commitResponse, commitRequest)
	if commitResponse.Code != http.StatusOK {
		t.Fatalf("seed committed revision status=%d body=%s", commitResponse.Code, commitResponse.Body.String())
	}
	statePath := state.statePath
	previous, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read previous state: %v", err)
	}
	revisions, err := state.applyRevisions.Get()
	if err != nil {
		t.Fatalf("read previous revision: %v", err)
	}

	intendedSnapshot := mustStateSnapshot(t, state)
	intendedSnapshot.Settings.Domain = "interrupted.example.com"
	intended, err := managementstate.NewStore(statePath, state.cipher).Marshal(intendedSnapshot)
	if err != nil {
		t.Fatalf("marshal intended state: %v", err)
	}
	previousPath := statePath + ".pending-mutation.previous"
	journalPath := statePath + ".pending-mutation.json"
	if _, err := managementstate.NewStore(statePath, state.cipher).PrepareStateCommit(
		intended, revisions.Desired, revisions.Desired+1,
	); err != nil {
		t.Fatalf("prepare and publish intended state: %v", err)
	}
	journalBody, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatalf("read pending journal: %v", err)
	}
	var journal managementstate.PendingMutationJournal
	if err := json.Unmarshal(journalBody, &journal); err != nil {
		t.Fatalf("decode pending journal: %v", err)
	}
	if journal.PreviousRevision != revisions.Desired || journal.IntendedRevision != revisions.Desired+1 {
		t.Fatalf("journal revision boundary=%d->%d want=%d->%d", journal.PreviousRevision, journal.IntendedRevision, revisions.Desired, revisions.Desired+1)
	}
	if journal.IntendedStateSHA256 != managementstate.EncodedStateSHA256(intended) {
		t.Fatalf("journal intended digest=%q", journal.IntendedStateSHA256)
	}
	if _, err := managementstate.NewStore(statePath, state.cipher).PrepareStateCommit(
		intended, revisions.Desired, revisions.Desired+1,
	); err == nil {
		t.Fatal("second state commit replaced an existing pending journal")
	}
	if err := closeClientSubsystem(state); err != nil {
		t.Fatalf("simulate process exit: %v", err)
	}

	restarted := newManagementState(ServerInfo{
		Version:   "test",
		Mode:      "dev",
		StatePath: statePath,
		ApplyRoot: filepath.Dir(statePath),
	})
	defer closeClientSubsystem(restarted)
	if restarted.settings.Domain == "interrupted.example.com" {
		t.Fatal("startup accepted state published by an uncommitted mutation")
	}
	recovered, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read recovered state: %v", err)
	}
	if !bytes.Equal(recovered, previous) {
		t.Fatalf("startup recovery did not restore previous state byte-for-byte")
	}
	for _, path := range []string{journalPath, previousPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("recovery artifact still exists at %s: %v", path, err)
		}
	}
	revisionsAfter, err := restarted.applyRevisions.Get()
	if err != nil {
		t.Fatalf("read revision after recovery: %v", err)
	}
	if revisionsAfter != revisions {
		t.Fatalf("revision changed during rollback recovery: before=%+v after=%+v", revisions, revisionsAfter)
	}
}

func TestStartupRejectsStateFileThatDoesNotMatchDesiredRevision(t *testing.T) {
	router, state := newApplyTrackedRouterWithState(t)
	request := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewBufferString(
		`{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"committed.example.com"}`,
	))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("create committed revision status=%d body=%s", response.Code, response.Body.String())
	}

	unrecorded := mustStateSnapshot(t, state)
	unrecorded.Settings.Domain = "unrecorded.example.com"
	encoded, err := managementstate.NewStore(state.statePath, state.cipher).Marshal(unrecorded)
	if err != nil {
		t.Fatalf("marshal unrecorded state: %v", err)
	}
	if err := managementstate.NewStore(state.statePath, state.cipher).SaveEncoded(encoded); err != nil {
		t.Fatalf("replace state outside commit protocol: %v", err)
	}
	if err := closeClientSubsystem(state); err != nil {
		t.Fatalf("close panel database before restart: %v", err)
	}

	restarted := newManagementState(ServerInfo{
		Version:   "test",
		Mode:      "dev",
		StatePath: state.statePath,
		ApplyRoot: filepath.Dir(state.statePath),
	})
	defer closeClientSubsystem(restarted)
	if !restarted.startupStateLoadFailed {
		t.Fatal("startup accepted a state file whose digest does not match the desired revision")
	}
	if restarted.settings.Domain == "unrecorded.example.com" {
		t.Fatal("unrecorded state became active in memory")
	}
}

func TestStartupBindsDigestForConsistentLegacySnapshot(t *testing.T) {
	router, state := newApplyTrackedRouterWithState(t)
	request := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewBufferString(
		`{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"legacy-consistent.example.com"}`,
	))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("create committed revision status=%d body=%s", response.Code, response.Body.String())
	}
	revisions, err := state.applyRevisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.db.Exec(`UPDATE revision_snapshots SET state_sha256='' WHERE revision=?`, revisions.Desired); err != nil {
		t.Fatalf("simulate pre-v4 snapshot row: %v", err)
	}
	statePath := state.statePath
	if err := closeClientSubsystem(state); err != nil {
		t.Fatalf("close panel database before restart: %v", err)
	}

	restarted := newManagementState(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: filepath.Dir(statePath)})
	defer closeClientSubsystem(restarted)
	if restarted.startupStateLoadFailed {
		t.Fatal("startup rejected a semantically consistent legacy snapshot")
	}
	if restarted.settings.Domain != "legacy-consistent.example.com" {
		t.Fatalf("restarted domain=%q", restarted.settings.Domain)
	}
	digest, err := restarted.applySnapshots.StateDigest(revisions.Desired)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest, err := stateFileDigest(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if digest != wantDigest {
		t.Fatalf("backfilled digest=%q want=%q", digest, wantDigest)
	}
}

func TestClientMutationBindsUnchangedStateFileDigest(t *testing.T) {
	router, state := newApplyTrackedRouterWithState(t)
	stateBody, err := os.ReadFile(state.statePath)
	if err != nil {
		t.Fatal(err)
	}
	response := v1Request(t, router, http.MethodPost, "/api/v1/clients", `{"name":"digest-client"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("create client status=%d body=%s", response.Code, response.Body.String())
	}
	revisions, err := state.applyRevisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	digest, err := state.applySnapshots.StateDigest(revisions.Desired)
	if err != nil {
		t.Fatal(err)
	}
	if digest != managementstate.EncodedStateSHA256(stateBody) {
		t.Fatalf("client revision state digest=%q", digest)
	}
}

func TestStartupFinalizesCommitInterruptedAfterSQLiteCommit(t *testing.T) {
	_, state := newApplyTrackedRouterWithState(t)
	statePath := state.statePath
	revisions, err := state.applyRevisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	intended := mustStateSnapshot(t, state)
	intended.Settings.Domain = "committed-before-crash.example.com"
	store := managementstate.NewStore(statePath, state.cipher)
	encoded, err := store.Marshal(intended)
	if err != nil {
		t.Fatal(err)
	}
	commit, err := store.PrepareStateCommit(encoded, revisions.Desired, revisions.Desired+1)
	if err != nil {
		t.Fatal(err)
	}
	state.settings.Domain = intended.Settings.Domain
	if _, err := state.bumpDesiredRevisionLocked(commit.Journal().IntendedStateSHA256); err != nil {
		t.Fatalf("commit SQLite side: %v", err)
	}
	// Deliberately abandon commit.Finalize(), equivalent to process interruption
	// after SQLite commit and before marker cleanup.
	if err := closeClientSubsystem(state); err != nil {
		t.Fatalf("simulate process exit: %v", err)
	}

	restarted := newManagementState(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: filepath.Dir(statePath)})
	defer closeClientSubsystem(restarted)
	if restarted.startupStateLoadFailed {
		t.Fatal("startup failed to finalize the committed pending mutation")
	}
	if restarted.settings.Domain != intended.Settings.Domain {
		t.Fatalf("recovered domain=%q want=%q", restarted.settings.Domain, intended.Settings.Domain)
	}
	for _, path := range []string{statePath + ".pending-mutation.json", statePath + ".pending-mutation.previous"} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("finalized recovery artifact still exists at %s: %v", path, err)
		}
	}
}
