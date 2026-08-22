package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	veilapply "github.com/mikkelchokolate/Veil/internal/apply"
	"github.com/mikkelchokolate/Veil/internal/privileged"
)

func useDefaultFenceRequiredHelper(t *testing.T, state *managementState) {
	t.Helper()
	policy := privileged.DefaultPolicy()
	stateRoot := filepath.Dir(state.statePath)
	policy.StagingRoot = filepath.Join(state.applyRoot, "generated")
	policy.GeneratedRoot = state.liveRoot
	policy.StateRoot = stateRoot
	policy.StatePath = state.statePath
	policy.KeyPath = state.keyPath
	policy.BackupPassphrasePath = state.backupPassphrasePath
	policy.BackupRoot = state.backupDir
	policy.UpdateRoot = filepath.Join(stateRoot, "updates")
	policy.FencePath = filepath.Join(stateRoot, "transactions", "runtime-fence.json")
	policy.AllowedArtifactNames = allowedArtifactNamesFromState(state)
	if !policy.RequireFence {
		t.Fatal("DefaultPolicy no longer requires a runtime fence")
	}
	executor := privileged.NewProductionExecutor(privileged.ProductionConfig{
		PromotionBackupRoot: filepath.Join(state.applyRoot, "promotion-backups"),
		StatePath:           state.statePath,
		KeyPath:             state.keyPath,
		BackupRoot:          state.backupDir,
		VeilVersion:         "test",
	})
	executor.ServiceAction = func(context.Context, privileged.ServiceActionRequest) error { return nil }
	executor.ServiceStatus = func(_ context.Context, request privileged.ServiceStatusRequest) (privileged.ServiceStatusResult, error) {
		result := privileged.ServiceStatusResult{}
		for _, unit := range request.Units {
			result.Services = append(result.Services, privileged.ServiceStatus{Unit: unit, LoadState: "loaded", ActiveState: "active", SubState: "running"})
		}
		return result, nil
	}
	executor.Firewall = func(_ context.Context, request privileged.ResolvedFirewall) (privileged.FirewallResult, error) {
		result := privileged.FirewallResult{TransactionID: request.TransactionID}
		if request.Action == privileged.FirewallActionPrepare {
			result.TransactionID = "test-firewall-transaction"
			result.Prepared = true
		}
		return result, nil
	}
	executor.CaddyLoad = func(context.Context, privileged.CaddyLoadRequest) error { return nil }
	executor.SyncCaddyCert = func(context.Context, privileged.SyncCaddyCertRequest) (privileged.SyncCaddyCertResult, error) {
		return privileged.SyncCaddyCertResult{}, nil
	}
	state.privileged = privileged.NewLocalAdapter(policy, executor)
	state.privilegedLocal = false
}

func createFenceTestInbound(t *testing.T, router http.Handler) {
	t.Helper()
	response := v1Request(t, router, http.MethodPost, "/api/inbounds",
		`{"name":"fence-hy","protocol":"hysteria2","transport":"udp","port":31443,"enabled":true}`)
	if response.Code != http.StatusCreated && response.Code != http.StatusOK {
		t.Fatalf("create fence inbound: %d %s", response.Code, response.Body.String())
	}
}

func TestHTTPManualApplyUsesDurableRunnerAgainstDefaultFencePolicy(t *testing.T) {
	router, state := newApplyTrackedRouterWithState(t)
	createFenceTestInbound(t, router)
	useDefaultFenceRequiredHelper(t, state)
	before, err := state.applyJobs.List(100)
	if err != nil {
		t.Fatal(err)
	}
	response := v1Request(t, router, http.MethodPost, "/api/apply", `{"confirm":true,"applyLive":true,"applyServices":true}`)
	if response.Code != http.StatusOK {
		t.Fatalf("manual apply did not pass RequireFence=true helper: %d %s", response.Code, response.Body.String())
	}
	after, err := state.applyJobs.List(100)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before)+1 {
		t.Fatalf("manual apply jobs=%d -> %d, want exactly one durable Runner job", len(before), len(after))
	}
	job := after[0]
	if job.Status != veilapply.StatusSucceeded || job.LeaseGeneration == 0 || job.OwnerProcess == "" {
		t.Fatalf("manual apply was not durably fenced and finalized: %+v", job)
	}
}

func TestHTTPDirectServiceRestartUsesSameDurableRunner(t *testing.T) {
	router, state := newApplyTrackedRouterWithState(t)
	createFenceTestInbound(t, router)
	useDefaultFenceRequiredHelper(t, state)
	beforeRevisions, err := state.applyRevisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	state.mu.Lock()
	pendingRevision, err := state.bumpDesiredRevisionLocked()
	state.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if pendingRevision <= beforeRevisions.Applied {
		t.Fatalf("pending revision=%d applied=%d", pendingRevision, beforeRevisions.Applied)
	}
	before, err := state.applyJobs.List(100)
	if err != nil {
		t.Fatal(err)
	}
	response := v1Request(t, router, http.MethodPost, "/api/services/hysteria2-fence-hy/restart", `{"confirm":true}`)
	if response.Code != http.StatusOK {
		t.Fatalf("service restart: %d %s", response.Code, response.Body.String())
	}
	after, err := state.applyJobs.List(100)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before)+1 {
		t.Fatalf("direct service mutation jobs=%d -> %d, want one Runner job", len(before), len(after))
	}
	if after[0].LeaseGeneration == 0 || !strings.HasPrefix(after[0].Trigger, "service") {
		t.Fatalf("direct service job lacks common fence/trigger: %+v", after[0])
	}
	afterRevisions, err := state.applyRevisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	if afterRevisions.Applied != pendingRevision || afterRevisions.Desired != pendingRevision {
		t.Fatalf("service side effect did not converge pending config revision first: before=%+v after=%+v", beforeRevisions, afterRevisions)
	}
}

func TestRevisionSnapshotStoresDeterministicEffectiveTime(t *testing.T) {
	router, state := newApplyTrackedRouterWithState(t)
	response := v1Request(t, router, http.MethodPost, "/api/v1/clients", `{"name":"effective-time"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("create client: %d %s", response.Code, response.Body.String())
	}
	revisions, err := state.applyRevisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := state.applySnapshots.Load(revisions.Desired)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot map[string]any
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		t.Fatal(err)
	}
	effective, ok := snapshot["effectiveAt"].(float64)
	if !ok || effective <= 0 {
		t.Fatalf("revision %d snapshot has no documented deterministic effectiveAt: %s", revisions.Desired, payload)
	}
}

func TestExpiredClientIsRemovedFromLiveRuntimeWithoutUserMutation(t *testing.T) {
	router, state := newApplyTrackedRouterWithState(t)
	// Drive the boundary synchronously: under -race with coverage, a two-second
	// wall-clock deadline can pass while the initial apply is still publishing.
	// Stopping the background worker and moving only the persisted deadline
	// preserves the production reconciliation path without timing the fixture.
	state.expirationReconciler.Stop()
	inbound := v1Request(t, router, http.MethodPost, "/api/inbounds",
		`{"name":"expiry-hy","protocol":"hysteria2","transport":"udp","port":32443,"enabled":true}`)
	if inbound.Code != http.StatusCreated && inbound.Code != http.StatusOK {
		t.Fatalf("create inbound: %d %s", inbound.Code, inbound.Body.String())
	}
	expires := time.Now().UTC().Add(30 * time.Second).Unix()
	created := v1Request(t, router, http.MethodPost, "/api/v1/clients", fmt.Sprintf(
		`{"name":"expiry-client","expiresAt":%d,"bindings":[{"inboundId":"expiry-hy","runtimeIdentity":"expiry_runtime_identity","credential":"expiry-secret"}]}`, expires))
	if created.Code != http.StatusCreated {
		t.Fatalf("create expiring client: %d %s", created.Code, created.Body.String())
	}
	body := unwrapClient(t, created.Body.Bytes())
	clientID, _ := body["id"].(string)
	before, err := state.applyRevisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	if !snapshotTreeContains(state.liveRoot, "expiry_runtime_identity") {
		t.Fatal("precondition: expiring identity was not published to the live runtime")
	}
	crossedAt := time.Now().UTC().Add(-time.Second).Unix()
	if _, err := state.db.Exec(`UPDATE clients SET expires_at=? WHERE id=?`, crossedAt, clientID); err != nil {
		t.Fatal(err)
	}
	if err := state.expirationReconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("reconcile crossed expiry boundary: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var status string
		var desiredRevision, appliedRevision uint64
		err := state.db.QueryRow(`SELECT state,desired_revision,applied_revision FROM expiration_enforcement WHERE client_id=?`, clientID).
			Scan(&status, &desiredRevision, &appliedRevision)
		if err == nil && status == "enforced" && desiredRevision > before.Desired && appliedRevision == desiredRevision &&
			!snapshotTreeContains(state.liveRoot, "expiry_runtime_identity") {
			return
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) && !strings.Contains(err.Error(), "no such table") {
			t.Fatalf("read expiration enforcement: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	current, _ := state.applyRevisions.Get()
	var enforcement any
	_ = state.db.QueryRow(`SELECT state FROM expiration_enforcement WHERE client_id=?`, clientID).Scan(&enforcement)
	t.Fatalf("expiry crossed without durable applied enforcement: revisions=%+v enforcement=%v liveIdentity=%v", current, enforcement, snapshotTreeContains(state.liveRoot, "expiry_runtime_identity"))
}

func snapshotTreeContains(root, needle string) bool {
	found := false
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || !info.Mode().IsRegular() {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr == nil && strings.Contains(string(body), needle) {
			found = true
		}
		return nil
	})
	return found
}

func TestCredentialRouteRejectsUnsupportedKind(t *testing.T) {
	router, _ := newApplyTrackedRouterWithState(t)
	createFenceTestInbound(t, router)
	clientID := createV1ClientWithBinding(t, router, "kind-client", "fence-hy", "original")
	view := getV1ClientMap(t, router, clientID)
	bindingID := firstBindingID(t, view)
	response := v1Request(t, router, http.MethodPost, "/api/v1/clients/"+clientID+"/credentials/"+bindingID,
		`{"kind":"arbitrary-private-key","value":"not-supported"}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unsupported credential kind status=%d, want 400: %s", response.Code, response.Body.String())
	}
}

func TestNestedBindingAndCredentialMutationsValidateClientOwner(t *testing.T) {
	operations := []struct {
		name   string
		method string
		suffix string
		body   func(version int) string
	}{
		{name: "patch binding", method: http.MethodPatch, suffix: "bindings", body: func(version int) string { return fmt.Sprintf(`{"enabled":false,"version":%d}`, version) }},
		{name: "delete binding", method: http.MethodDelete, suffix: "bindings", body: func(int) string { return "" }},
		{name: "set credential", method: http.MethodPost, suffix: "credentials", body: func(int) string { return `{"kind":"password","value":"cross-client"}` }},
		{name: "rotate credential", method: http.MethodPost, suffix: "credentials-rotate", body: func(int) string { return `{"kind":"password","value":"cross-client"}` }},
	}
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			router, state := newApplyTrackedRouterWithState(t)
			createFenceTestInbound(t, router)
			firstClient := createV1ClientWithBinding(t, router, "owner-a", "fence-hy", "owner-a-secret")
			secondClient := createV1ClientWithBinding(t, router, "owner-b", "fence-hy", "owner-b-secret")
			firstView := getV1ClientMap(t, router, firstClient)
			bindingID := firstBindingID(t, firstView)
			version := firstBindingVersion(t, firstView)
			path := "/api/v1/clients/" + secondClient + "/"
			switch operation.suffix {
			case "bindings":
				path += "bindings/" + bindingID
			case "credentials":
				path += "credentials/" + bindingID
			case "credentials-rotate":
				path += "credentials/" + bindingID + "/rotate"
			}
			response := v1Request(t, router, operation.method, path, operation.body(version))
			if response.Code != http.StatusNotFound {
				t.Fatalf("cross-client mutation status=%d, want 404: %s", response.Code, response.Body.String())
			}
			binding, err := state.clientRepo.GetBinding(bindingID)
			if err != nil {
				t.Fatalf("owner binding was mutated/deleted: %v", err)
			}
			if binding.ClientID != firstClient || !binding.Enabled || binding.Version != version {
				t.Fatalf("owner binding changed through cross-client route: %+v", binding)
			}
			active, err := state.clientCreds.ActiveForBinding(bindingID, "password")
			if err != nil {
				t.Fatalf("owner credential missing after cross-client route: %v", err)
			}
			plain, err := state.clientCreds.Reveal(active.ID)
			if err != nil || plain != "owner-a-secret" {
				t.Fatalf("owner credential changed through cross-client route: plaintext=%q err=%v", plain, err)
			}
		})
	}
}

func getV1ClientMap(t *testing.T, router http.Handler, id string) map[string]any {
	t.Helper()
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/clients/"+id, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("get client %s: %d %s", id, response.Code, response.Body.String())
	}
	return unwrapClient(t, response.Body.Bytes())
}

func firstBindingID(t *testing.T, view map[string]any) string {
	t.Helper()
	bindings, _ := view["bindings"].([]any)
	if len(bindings) != 1 {
		t.Fatalf("bindings=%#v", view["bindings"])
	}
	binding, _ := bindings[0].(map[string]any)
	id, _ := binding["id"].(string)
	if id == "" {
		t.Fatalf("binding id missing: %#v", binding)
	}
	return id
}

func firstBindingVersion(t *testing.T, view map[string]any) int {
	t.Helper()
	bindings := view["bindings"].([]any)
	binding := bindings[0].(map[string]any)
	version, _ := binding["version"].(float64)
	return int(version)
}
