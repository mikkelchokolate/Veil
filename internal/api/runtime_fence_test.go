package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/privileged"
	"github.com/mikkelchokolate/Veil/internal/statecommit"
)

func newFencedPanelState(t *testing.T, client privileged.Client) *managementState {
	t.Helper()
	root := t.TempDir()
	state := newManagementState(ServerInfo{
		Version: "test", Mode: "dev",
		StatePath:               filepath.Join(root, "state.json"),
		KeyPath:                 filepath.Join(root, "state.key"),
		ApplyRoot:               root,
		Privileged:              client,
		RequirePrivilegedHelper: true,
	})
	t.Cleanup(func() { closeClientSubsystem(state) })
	if state.db == nil {
		t.Fatal("fencing tests require the durable apply lease store")
	}
	return state
}

// TestAcquireRuntimeFenceMintsAndReleasesDurableLease proves panel-side
// privileged mutations are backed by the durable apply lease: while held, a
// second acquisition is refused, and release makes it acquirable again.
func TestAcquireRuntimeFenceMintsAndReleasesDurableLease(t *testing.T) {
	state := newFencedPanelState(t, &recordingPrivilegedClient{})
	token, release, err := state.acquireRuntimeFence("test-operation")
	if err != nil {
		t.Fatalf("acquireRuntimeFence: %v", err)
	}
	if token.Owner == "" || token.Generation == 0 || token.OperationID == "" || token.LeaseExpiresAt <= time.Now().Unix() {
		t.Fatalf("incomplete fencing token: %+v", token)
	}
	if _, _, err := state.acquireRuntimeFence("other-operation"); err == nil {
		t.Fatal("second fencing lease acquisition succeeded while the first was held")
	}
	release()
	token2, release2, err := state.acquireRuntimeFence("other-operation")
	if err != nil {
		t.Fatalf("lease was not released: %v", err)
	}
	defer release2()
	if token2.Generation <= token.Generation {
		t.Fatalf("fencing generation did not advance: %d -> %d", token.Generation, token2.Generation)
	}
}

// TestHandleRotateKeySendsDurableFenceToken locks in #982: the helper request
// carries a live fencing token minted from the durable apply lease.
func TestHandleRotateKeySendsDurableFenceToken(t *testing.T) {
	client := &recordingPrivilegedClient{}
	state := newFencedPanelState(t, client)
	admin, err := state.sessionRegistry().Create(SessionCreateInput{Username: "admin", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	request := adminJSONRequest(http.MethodPost, "/api/admin/rotate-key", `{}`)
	request.AddCookie(&http.Cookie{Name: "veil_session", Value: admin.Token})
	response := httptest.NewRecorder()
	state.handleRotateKey(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("rotate status=%d body=%s", response.Code, response.Body.String())
	}
	if len(client.rotateRequests) != 1 {
		t.Fatalf("rotate requests=%d, want 1", len(client.rotateRequests))
	}
	fence := client.rotateRequests[0].Fence
	if fence.Owner == "" || fence.Generation == 0 || fence.OperationID == "" {
		t.Fatalf("rotation sent an unfenced request: %+v", fence)
	}
}

// TestRecoverPendingKeyRotationToleratesFenceRejectionWithoutJournal covers
// the startup probe path: with no pending journal and no lease store the
// helper may reject the unfenced call, and startup must still proceed.
func TestRecoverPendingKeyRotationToleratesFenceRejectionWithoutJournal(t *testing.T) {
	root := t.TempDir()
	client := &recordingPrivilegedClient{
		recoverErr: &privileged.Error{Code: privileged.ErrorConflict, Message: "runtime mutation requires a fencing token"},
	}
	state := &managementState{
		statePath:  filepath.Join(root, "state.json"),
		privileged: client,
	}
	lifecycle := NewManagementStateLifecycle(state)
	if err := lifecycle.RecoverPendingKeyRotationContext(context.Background()); err != nil {
		t.Fatalf("unfenced no-op recovery probe must be tolerated: %v", err)
	}
	if client.recoverRotationCalls != 1 {
		t.Fatalf("recovery calls=%d, want 1", client.recoverRotationCalls)
	}
}

// TestRecoverPendingKeyRotationPropagatesFenceRejectionWhenJournalPending
// proves a pending privileged journal cannot be recovered without a valid
// fencing token: the rejection surfaces instead of being swallowed.
func TestRecoverPendingKeyRotationPropagatesFenceRejectionWhenJournalPending(t *testing.T) {
	root := t.TempDir()
	state := &managementState{
		statePath: filepath.Join(root, "state.json"),
		privileged: &recordingPrivilegedClient{
			recoverErr: &privileged.Error{Code: privileged.ErrorConflict, Message: "runtime mutation requires a fencing token"},
		},
	}
	journalPath := statecommit.KeyRotationJournalPath(state.statePath)
	if err := os.WriteFile(journalPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	lifecycle := NewManagementStateLifecycle(state)
	if err := lifecycle.RecoverPendingKeyRotationContext(context.Background()); err == nil {
		t.Fatal("pending journal recovery must propagate the fencing rejection")
	}
}

// TestRecoverPendingKeyRotationMintsFenceBeforeHelperCall locks in the other
// half of #982: startup recovery mints a durable lease-backed token for the
// helper instead of calling it unfenced.
func TestRecoverPendingKeyRotationMintsFenceBeforeHelperCall(t *testing.T) {
	client := &recordingPrivilegedClient{}
	state := newFencedPanelState(t, client)
	journalPath := statecommit.KeyRotationJournalPath(state.statePath)
	if err := os.WriteFile(journalPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	lifecycle := NewManagementStateLifecycle(state)
	if err := lifecycle.RecoverPendingKeyRotationContext(context.Background()); err != nil {
		t.Fatalf("pending recovery: %v", err)
	}
	if len(client.recoverRequests) == 0 {
		t.Fatal("helper recovery was not invoked")
	}
	fence := client.recoverRequests[len(client.recoverRequests)-1].Fence
	if fence.Owner == "" || fence.Generation == 0 || fence.OperationID == "" {
		t.Fatalf("recovery sent an unfenced request: %+v", fence)
	}
	// The lease must be released when recovery returns.
	postToken, postRelease, err := state.acquireRuntimeFence("post-recovery")
	if err != nil {
		t.Fatalf("recovery fencing lease was not released: %v", err)
	}
	defer postRelease()
	if postToken.Generation <= fence.Generation {
		t.Fatalf("fencing generation did not advance across recovery: %d -> %d", fence.Generation, postToken.Generation)
	}
}

// TestBackupMutationsSendDurableFenceToken locks in #985: create, prune, and
// delete carry a live fencing token minted from the durable apply lease.
func TestBackupMutationsSendDurableFenceToken(t *testing.T) {
	client := &recordingPrivilegedClient{}
	state := newPanelBackupState(t)
	state.mu.Lock()
	state.privileged = client
	state.mu.Unlock()

	create := adminJSONRequest(http.MethodPost, "/api/backups", `{"prune":true,"daily":7,"weekly":4,"monthly":6}`)
	createResponse := httptest.NewRecorder()
	state.handleBackups(createResponse, create)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createResponse.Code, createResponse.Body.String())
	}
	var createFence privileged.FenceToken
	for _, request := range client.backups {
		if request.Action == privileged.BackupActionCreate {
			createFence = request.Fence
		}
	}
	if createFence.Owner == "" || createFence.Generation == 0 {
		t.Fatalf("backup create sent an unfenced request: %+v", client.backups)
	}

	prune := adminJSONRequest(http.MethodPost, "/api/backups/prune", `{"daily":7,"weekly":4,"monthly":6}`)
	pruneResponse := httptest.NewRecorder()
	state.handleBackupPrune(pruneResponse, prune)
	if pruneResponse.Code != http.StatusOK {
		t.Fatalf("prune status=%d body=%s", pruneResponse.Code, pruneResponse.Body.String())
	}

	remove := adminJSONRequest(http.MethodDelete, "/api/backups/veil_backup_20260605_120000.tar.gz.enc", "")
	removeResponse := httptest.NewRecorder()
	state.handleBackupByName(removeResponse, remove)
	if removeResponse.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", removeResponse.Code, removeResponse.Body.String())
	}

	var sawPrune, sawDelete bool
	for _, request := range client.backups {
		switch request.Action {
		case privileged.BackupActionPrune:
			sawPrune = true
			if request.Fence.Owner == "" || request.Fence.Generation == 0 {
				t.Fatalf("backup prune sent an unfenced request: %+v", request.Fence)
			}
		case privileged.BackupActionDelete:
			sawDelete = true
			if request.Fence.Owner == "" || request.Fence.Generation == 0 {
				t.Fatalf("backup delete sent an unfenced request: %+v", request.Fence)
			}
		}
	}
	if !sawPrune || !sawDelete {
		t.Fatalf("missing prune/delete requests in %+v", client.backups)
	}
	// Read-only actions must stay unfenced so passive checks never acquire the
	// mutation lease.
	list := adminJSONRequest(http.MethodGet, "/api/backups", "")
	listResponse := httptest.NewRecorder()
	state.handleBackups(listResponse, list)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listResponse.Code, listResponse.Body.String())
	}
	for _, request := range client.backups {
		if request.Action == privileged.BackupActionList && request.Fence.Owner != "" {
			t.Fatalf("read-only backup list carried a fencing token: %+v", request.Fence)
		}
	}
}
