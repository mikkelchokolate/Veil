package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	veilapply "github.com/mikkelchokolate/Veil/internal/apply"
	"github.com/mikkelchokolate/Veil/internal/client"
	"github.com/mikkelchokolate/Veil/internal/privileged"
)

type blockingRestorePrivilegedClient struct {
	*recordingPrivilegedClient
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (c *blockingRestorePrivilegedClient) Backup(ctx context.Context, request privileged.BackupRequest) (privileged.BackupResult, error) {
	if request.Action != privileged.BackupActionRestore {
		return c.recordingPrivilegedClient.Backup(ctx, request)
	}
	c.once.Do(func() { close(c.started) })
	select {
	case <-c.release:
		return privileged.BackupResult{Restored: false}, errors.New("injected restore stop")
	case <-ctx.Done():
		return privileged.BackupResult{}, ctx.Err()
	}
}

func TestSlowRestoreDoesNotHoldManagementStateMutex(t *testing.T) {
	_, state := newApplyTrackedRouterWithState(t)
	client := &blockingRestorePrivilegedClient{
		recordingPrivilegedClient: &recordingPrivilegedClient{},
		started:                   make(chan struct{}),
		release:                   make(chan struct{}),
	}
	state.privileged = client
	state.privilegedLocal = false
	jobID := "slow-restore-lock-regression"
	state.backupJobsMu.Lock()
	state.backupJobs[jobID] = BackupRestoreJob{ID: jobID, Archive: "test.enc", Status: "queued", CreatedAt: time.Now().UTC()}
	if err := state.persistBackupRestoreJobsLocked(); err != nil {
		state.backupJobsMu.Unlock()
		t.Fatal(err)
	}
	state.backupJobsMu.Unlock()
	state.backupMutationMu.Lock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		state.runPanelBackupRestore(jobID, "test.enc", "", "admin", "admin", "127.0.0.1", "test")
	}()
	defer func() {
		close(client.release)
		<-done
	}()
	select {
	case <-client.started:
	case <-time.After(2 * time.Second):
		t.Fatal("restore helper did not start")
	}
	assertSettingsReadCompletesWhileSlowWorkBlocked(t, state)
}

func TestQuotaTriggeredApplyDoesNotHoldManagementStateMutex(t *testing.T) {
	router, state := newApplyTrackedRouterWithState(t)
	inbound := postJSON(t, router, "/api/inbounds", `{"name":"lock-hy","protocol":"hysteria2","transport":"udp","port":29443,"enabled":true}`)
	if inbound.Code != http.StatusCreated {
		t.Fatalf("create inbound: %d %s", inbound.Code, inbound.Body.String())
	}
	createdResponse := v1Request(t, router, http.MethodPost, "/api/v1/clients", `{"name":"lock-client","quotaBytes":10,"bindings":[{"inboundId":"lock-hy","runtimeIdentity":"lock_identity","credential":"lock-secret"}]}`)
	if createdResponse.Code != http.StatusCreated {
		t.Fatalf("create client: %d %s", createdResponse.Code, createdResponse.Body.String())
	}
	created := unwrapClient(t, createdResponse.Body.Bytes())
	clientID, _ := created["id"].(string)
	bindingObjects, _ := created["bindings"].([]any)
	if clientID == "" || len(bindingObjects) != 1 {
		t.Fatalf("unexpected created client: %#v", created)
	}
	bindingObject, _ := bindingObjects[0].(map[string]any)
	bindingID, _ := bindingObject["id"].(string)
	if bindingID == "" {
		t.Fatalf("missing binding id: %#v", created)
	}
	if err := state.trafficStore.RecordSample(client.Sample{BindingID: bindingID, UploadBytes: 100, AtUnix: time.Now().Unix()}); err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	state.applyRunner = veilapply.NewRunner(state.applyRevisions, state.applyJobs, func(uint64) (veilapply.Result, error) {
		once.Do(func() { close(started) })
		<-release
		return veilapply.Result{Success: false}, errors.New("injected apply stop")
	})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = state.trafficReconciler.ReconcileOnce()
	}()
	defer func() {
		close(release)
		<-done
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("quota apply did not start")
	}
	assertSettingsReadCompletesWhileSlowWorkBlocked(t, state)
}

func assertSettingsReadCompletesWhileSlowWorkBlocked(t *testing.T, state *managementState) {
	t.Helper()
	completed := make(chan struct{})
	go func() {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
		state.handleSettings(response, request)
		close(completed)
	}()
	select {
	case <-completed:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("read-only settings request blocked behind slow restore/apply work while managementState.mu was held")
	}
}
