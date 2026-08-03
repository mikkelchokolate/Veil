package api

import (
	"encoding/json"
	"testing"
	"time"

	veilapply "github.com/mikkelchokolate/Veil/internal/apply"
	"github.com/mikkelchokolate/Veil/internal/client"
)

func TestQuotaRolloverCommitsPeriodStateSnapshotAndExactlyOneApplyJob(t *testing.T) {
	state := newClientLifecycleTestState(t)
	state.applyRunner.Close()
	state.applyRunner = veilapply.NewRunner(state.applyRevisions, state.applyJobs, veilapply.ExecutorFunc(func(uint64) (veilapply.Result, error) {
		return veilapply.Result{Success: true, Disposition: veilapply.ApplyDispositionRuntimeConverged, MarkRevisionLive: true}, nil
	}))
	quota := int64(100)
	expired := time.Now().UTC().Add(-40 * 24 * time.Hour).Unix()
	created, err := state.clientService.Create(client.Client{
		Name: "quota-atomic", Enabled: true, QuotaBytes: &quota,
		QuotaResetPolicy: client.ResetMonthly, QuotaResetAt: &expired, Depleted: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Unix()
	if err := state.trafficStore.RecordSample(client.Sample{
		ClientID: created.ID, UploadBytes: 80, DownloadBytes: 30, AtUnix: now,
	}); err != nil {
		t.Fatal(err)
	}
	before, err := state.applyRevisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	jobsBefore, err := state.applyJobs.List(100)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := state.trafficReconciler.ReconcileOnce()
	if err != nil {
		t.Fatal(err)
	}
	if changed != 1 {
		t.Fatalf("changed=%d want 1", changed)
	}
	after, err := state.applyRevisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	if after.Desired != before.Desired+1 || after.Applied != after.Desired {
		t.Fatalf("revision transition before=%+v after=%+v", before, after)
	}
	jobsAfter, err := state.applyJobs.List(100)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobsAfter) != len(jobsBefore)+1 {
		t.Fatalf("jobs before=%d after=%d, want exactly one", len(jobsBefore), len(jobsAfter))
	}
	restored, err := state.clientService.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Depleted || restored.QuotaResetAt == nil || *restored.QuotaResetAt <= now {
		t.Fatalf("rollover state not advanced: %+v", restored.Client)
	}
	upload, download, err := state.trafficStore.TotalsForClient(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if upload != 0 || download != 0 {
		t.Fatalf("current-period usage=(%d,%d), want reset", upload, download)
	}
	history, err := state.trafficStore.HistoryForClient(created.ID, 0, now+3600, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].UploadDelta != 80 || history[0].DownloadDelta != 30 {
		t.Fatalf("lifetime traffic history was deleted or changed: %+v", history)
	}
	payload, err := state.applySnapshots.Load(after.Desired)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot managementSnapshot
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range snapshot.Clients {
		if item.ID != created.ID {
			continue
		}
		found = true
		if item.Depleted || item.QuotaResetAt == nil || *item.QuotaResetAt != *restored.QuotaResetAt {
			t.Fatalf("immutable quota snapshot mismatch: %+v current=%+v", item, restored.Client)
		}
	}
	if !found {
		t.Fatal("rolled-over client missing from immutable snapshot")
	}
}
