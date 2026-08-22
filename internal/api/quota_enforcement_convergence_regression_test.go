package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	veilapply "github.com/mikkelchokolate/Veil/internal/apply"
	"github.com/mikkelchokolate/Veil/internal/client"
)

func TestQuotaEnforcementRetriesUntilDepletedRevisionIsApplied(t *testing.T) {
	router, state := newApplyTrackedRouterWithState(t)
	t.Cleanup(func() { _ = state.Close() })
	inbound := v1Request(t, router, http.MethodPost, "/api/inbounds",
		`{"name":"quota-retry-hy","protocol":"hysteria2","transport":"udp","port":26443,"enabled":true}`)
	if inbound.Code != http.StatusCreated && inbound.Code != http.StatusOK {
		t.Fatalf("create inbound: %d %s", inbound.Code, inbound.Body.String())
	}
	createdResponse := v1Request(t, router, http.MethodPost, "/api/v1/clients",
		`{"name":"quota-retry-client","quotaBytes":100,"bindings":[{"inboundId":"quota-retry-hy","runtimeIdentity":"quota_retry_identity","credential":"credential"}]}`)
	if createdResponse.Code != http.StatusCreated {
		t.Fatalf("create client: %d %s", createdResponse.Code, createdResponse.Body.String())
	}
	created := unwrapClient(t, createdResponse.Body.Bytes())
	clientID := created["id"].(string)
	bindings := created["bindings"].([]any)
	bindingID := bindings[0].(map[string]any)["id"].(string)
	before, err := state.applyRevisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	if err := state.trafficStore.RecordSample(client.Sample{
		BindingID: bindingID, ClientID: clientID, UploadBytes: 101, DownloadBytes: 0, AtUnix: 1,
	}); err != nil {
		t.Fatal(err)
	}

	state.applyRunner.Close()
	state.applyRunner = veilapply.NewRunner(state.applyRevisions, state.applyJobs, veilapply.ExecutorFunc(func(uint64) (veilapply.Result, error) {
		return veilapply.Result{Success: false}, errors.New("simulated quota runtime apply failure")
	}))
	changed, firstErr := state.trafficReconciler.ReconcileOnce()
	if changed != 0 && changed != 1 {
		t.Errorf("first reconcile changed = %d, want attempted transition", changed)
	}
	if firstErr == nil {
		t.Error("failed quota runtime apply was reported as successful")
	}
	depleted, err := state.clientRepo.Get(clientID)
	if err != nil {
		t.Fatal(err)
	}
	if !depleted.Depleted {
		t.Fatal("quota crossing did not persist Depleted=true")
	}
	failedRevision, err := state.applyRevisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	if failedRevision.Desired <= before.Desired || failedRevision.Applied != before.Applied {
		t.Errorf("failed enforcement revision = %+v, before=%+v", failedRevision, before)
	}
	var failedState string
	var nextRetryAt int64
	if err := state.db.QueryRow(`SELECT state, next_retry_at FROM quota_enforcement WHERE client_id=?`, clientID).Scan(&failedState, &nextRetryAt); err != nil {
		t.Errorf("load persisted failed enforcement state: %v", err)
	} else {
		if failedState != "failed" && failedState != "pending" {
			t.Errorf("failed enforcement state = %q, want failed or pending", failedState)
		}
		if nextRetryAt <= 0 {
			t.Errorf("nextRetryAt = %d, want scheduled retry", nextRetryAt)
		}
	}

	state.applyRunner.Close()
	state.applyRunner = veilapply.NewRunner(state.applyRevisions, state.applyJobs, state.executeApplyRevision)
	if _, err := state.db.Exec(`UPDATE quota_enforcement SET next_retry_at=0 WHERE client_id=?`, clientID); err != nil {
		t.Fatal(err)
	}
	if _, err := state.db.Exec(`CREATE TRIGGER fail_duplicate_quota_terminal BEFORE UPDATE OF state ON quota_enforcement
WHEN OLD.state='enforced' AND NEW.state='enforced' BEGIN SELECT RAISE(ABORT,'terminal persistence failure'); END`); err != nil {
		t.Fatal(err)
	}
	changed, retryErr := state.trafficReconciler.ReconcileOnce()
	if retryErr == nil {
		t.Errorf("expected duplicate terminal persistence failure: changed=%d", changed)
	}
	after, err := state.applyRevisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	if after.Desired != failedRevision.Desired {
		t.Errorf("quota retry generated a new revision: failed=%+v after=%+v", failedRevision, after)
	}
	if after.Applied != after.Desired || after.Applied < failedRevision.Desired {
		t.Errorf("retry did not converge applied state: failed=%+v after=%+v", failedRevision, after)
	}
	var enforcedState string
	if err := state.db.QueryRow(`SELECT state FROM quota_enforcement WHERE client_id=?`, clientID).Scan(&enforcedState); err != nil {
		t.Errorf("load enforced state: %v", err)
	} else if enforcedState != "enforced" {
		t.Errorf("enforcement state = %q, want enforced", enforcedState)
	}
	if _, err := state.db.Exec(`DROP TRIGGER fail_duplicate_quota_terminal`); err != nil {
		t.Fatal(err)
	}
	if changedAgain, err := state.trafficReconciler.ReconcileOnce(); err != nil || changedAgain != 0 {
		t.Errorf("enforced quota retried after terminal marker failure: changed=%d err=%v", changedAgain, err)
	}
	stable, err := state.applyRevisions.Get()
	if err != nil || stable.Desired != after.Desired {
		t.Errorf("terminal marker failure generated another revision: after=%+v stable=%+v err=%v", after, stable, err)
	}
	live := captureSnapshotTestTree(t, state.liveRoot)
	for path, file := range live {
		if strings.Contains(string(file.Body), "quota_retry_identity") {
			t.Errorf("depleted runtime identity remains live in %s", path)
		}
	}
}

func TestQuotaReconcilerContinuesAfterOneBrokenClient(t *testing.T) {
	db := openApplyTestDB(t)
	defer db.Close()
	repo := client.NewRepository(db)
	traffic := client.NewTrafficStore(db)
	quota := int64(1)
	var ids []string
	for i := 0; i < 2; i++ {
		row, err := repo.Create(client.Client{
			Name: fmt.Sprintf("quota-broken-%d", i), Enabled: true, QuotaBytes: &quota, QuotaResetPolicy: client.ResetNever,
		})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, row.ID)
		if err := traffic.RecordSample(client.Sample{ClientID: row.ID, UploadBytes: 2, AtUnix: 1}); err != nil {
			t.Fatal(err)
		}
	}
	attempted := map[string]bool{}
	reconciler := client.NewTransactionalReconciler(repo, traffic, 0, func(mutation client.QuotaMutation) error {
		attempted[mutation.ClientID] = true
		if mutation.ClientID == ids[0] {
			return errors.New("first client is broken")
		}
		return nil
	})
	_, err := reconciler.ReconcileOnce()
	if err == nil {
		t.Error("aggregate reconcile error = nil, want first-client error")
	}
	for _, id := range ids {
		if !attempted[id] {
			t.Errorf("client %s was not reconciled after another client failed", id)
		}
	}
}
