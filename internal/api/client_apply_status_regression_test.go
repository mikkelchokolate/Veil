package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	veilapply "github.com/mikkelchokolate/Veil/internal/apply"
)

func TestClientListAndDetailReflectApplyState(t *testing.T) {
	router, state := newApplyTrackedRouterWithState(t)
	succeedApply := veilapply.ExecutorFunc(func(uint64) (veilapply.Result, error) {
		return veilapply.Result{Success: true, Disposition: veilapply.ApplyDispositionRuntimeConverged, MarkRevisionLive: true}, nil
	})
	failApply := veilapply.ExecutorFunc(func(uint64) (veilapply.Result, error) {
		return veilapply.Result{Success: false}, errors.New("simulated apply validation failure")
	})
	state.applyRunner.Close()
	state.applyRunner = veilapply.NewRunner(state.applyRevisions, state.applyJobs, succeedApply)
	inbound := v1Request(t, router, http.MethodPost, "/api/inbounds",
		`{"name":"apply-status-hy","protocol":"hysteria2","transport":"udp","port":27501,"enabled":true}`)
	if inbound.Code != http.StatusCreated && inbound.Code != http.StatusOK {
		t.Fatalf("create inbound: %d %s", inbound.Code, inbound.Body.String())
	}
	liveResp := v1Request(t, router, http.MethodPost, "/api/v1/clients",
		`{"name":"already-applied","bindings":[{"inboundId":"apply-status-hy","credential":"live-pass"}]}`)
	if liveResp.Code != http.StatusCreated {
		t.Fatalf("create live client: %d %s", liveResp.Code, liveResp.Body.String())
	}
	liveID := unwrapClient(t, liveResp.Body.Bytes())["id"].(string)
	if got := clientStatusFromGet(t, router, liveID); got != "active" {
		t.Fatalf("applied client status=%q, want active", got)
	}

	state.applyRunner.Close()
	state.applyRunner = veilapply.NewRunner(state.applyRevisions, state.applyJobs, failApply)
	failedResp := v1Request(t, router, http.MethodPost, "/api/v1/clients",
		`{"name":"never-applied","bindings":[{"inboundId":"apply-status-hy","credential":"new-pass"}]}`)
	if failedResp.Code != http.StatusCreated {
		t.Fatalf("create failed client: %d %s", failedResp.Code, failedResp.Body.String())
	}
	failedID := unwrapClient(t, failedResp.Body.Bytes())["id"].(string)
	if got := unwrapClient(t, failedResp.Body.Bytes())["status"]; got != "apply_failed" {
		t.Fatalf("create response status=%v, want apply_failed", got)
	}
	if got := clientStatusFromGet(t, router, failedID); got != "apply_failed" {
		t.Fatalf("detail status=%q, want apply_failed", got)
	}
	if got := clientStatusFromGet(t, router, liveID); got != "active" {
		t.Fatalf("unrelated applied client status=%q, want active", got)
	}
	list := v1Request(t, router, http.MethodGet, "/api/v1/clients?pageSize=50", "")
	if list.Code != http.StatusOK {
		t.Fatalf("list: %d %s", list.Code, list.Body.String())
	}
	var body struct {
		Items []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	byID := map[string]string{}
	for _, item := range body.Items {
		byID[item.ID] = item.Status
	}
	if byID[failedID] != "apply_failed" {
		t.Fatalf("list status for never-applied=%q", byID[failedID])
	}
	if byID[liveID] != "active" {
		t.Fatalf("list status for applied=%q", byID[liveID])
	}

	state.applyRunner.Close()
	state.applyRunner = veilapply.NewRunner(state.applyRevisions, state.applyJobs, veilapply.ExecutorFunc(func(uint64) (veilapply.Result, error) {
		return veilapply.Result{Success: true, Disposition: veilapply.ApplyDispositionRuntimeConverged, MarkRevisionLive: true}, nil
	}))
	retry := v1Request(t, router, http.MethodPost, "/api/apply/reconcile", "")
	if retry.Code != http.StatusOK {
		t.Fatalf("reconcile: %d %s", retry.Code, retry.Body.String())
	}
	var reconcile struct {
		Reconciled bool `json:"reconciled"`
		ApplyJob   struct {
			ID              string `json:"id"`
			Status          string `json:"status"`
			DesiredRevision uint64 `json:"desiredRevision"`
		} `json:"applyJob"`
		Revision struct {
			Desired uint64 `json:"desired"`
			Applied uint64 `json:"applied"`
		} `json:"revision"`
		State struct {
			State           string `json:"state"`
			DesiredRevision uint64 `json:"desiredRevision"`
			AppliedRevision uint64 `json:"appliedRevision"`
		} `json:"state"`
	}
	if err := json.Unmarshal(retry.Body.Bytes(), &reconcile); err != nil {
		t.Fatalf("decode reconcile response: %v", err)
	}
	// The reconcile contract is synchronous: a 200 carries an explicit
	// reconciled flag plus post-run revisions and the derived state — never a
	// stale snapshot taken before the job ran.
	if !reconcile.Reconciled {
		t.Fatalf("reconciled flag=false in success envelope: %s", retry.Body.String())
	}
	if reconcile.Revision.Desired == 0 || reconcile.Revision.Desired != reconcile.Revision.Applied {
		t.Fatalf("reconcile revisions=%+v, want desired==applied>0", reconcile.Revision)
	}
	if reconcile.ApplyJob.ID == "" || reconcile.ApplyJob.Status != "succeeded" {
		t.Fatalf("reconcile applyJob=%+v, want a succeeded job", reconcile.ApplyJob)
	}
	if reconcile.ApplyJob.DesiredRevision != reconcile.Revision.Desired {
		t.Fatalf("reconcile job revision %d != desired %d", reconcile.ApplyJob.DesiredRevision, reconcile.Revision.Desired)
	}
	if reconcile.State.State != "synced" ||
		reconcile.State.DesiredRevision != reconcile.Revision.Desired ||
		reconcile.State.AppliedRevision != reconcile.Revision.Applied {
		t.Fatalf("reconcile state=%+v, want synced state matching revisions %+v", reconcile.State, reconcile.Revision)
	}
	if got := clientStatusFromGet(t, router, failedID); got != "active" {
		t.Fatalf("after successful retry status=%q, want active", got)
	}
}

func TestClientDetailPendingApplyWhenJobNotStarted(t *testing.T) {
	previous := autoApplyAfterMutation
	router, _ := newApplyTrackedRouterWithState(t)
	inbound := v1Request(t, router, http.MethodPost, "/api/inbounds",
		`{"name":"pending-apply-hy","protocol":"hysteria2","transport":"udp","port":27502,"enabled":true}`)
	if inbound.Code != http.StatusCreated && inbound.Code != http.StatusOK {
		t.Fatalf("create inbound: %d %s", inbound.Code, inbound.Body.String())
	}
	autoApplyAfterMutation = false
	t.Cleanup(func() { autoApplyAfterMutation = previous })
	created := v1Request(t, router, http.MethodPost, "/api/v1/clients",
		`{"name":"waiting-apply","bindings":[{"inboundId":"pending-apply-hy","credential":"pw"}]}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	id := unwrapClient(t, created.Body.Bytes())["id"].(string)
	if got := clientStatusFromGet(t, router, id); got != "pending_apply" {
		t.Fatalf("status=%q, want pending_apply", got)
	}
}

func clientStatusFromGet(t *testing.T, router http.Handler, id string) string {
	t.Helper()
	w := v1Request(t, router, http.MethodGet, "/api/v1/clients/"+id, "")
	if w.Code != http.StatusOK {
		t.Fatalf("get %s: %d %s", id, w.Code, w.Body.String())
	}
	var view struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	return view.Status
}
