package api

import (
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/client"
)

func newClientLifecycleTestState(t *testing.T) *managementState {
	t.Helper()
	originalFirewall := firewallApplierInstance
	originalRunner := serviceActionRunner
	originalHealth := serviceHealthChecker
	originalValidator := stagedConfigValidator
	firewallApplierInstance = &fakeFirewallApplier{}
	serviceActionRunner = func(command []string) ServiceActionResult {
		return ServiceActionResult{Name: command[len(command)-1], Command: append([]string(nil), command...), Success: true}
	}
	serviceHealthChecker = func(name string) ServiceHealthResult {
		return ServiceHealthResult{Name: name, Healthy: true}
	}
	stagedConfigValidator = func(paths []string) []ConfigValidationResult {
		results := make([]ConfigValidationResult, 0, len(paths))
		for _, path := range paths {
			results = append(results, ConfigValidationResult{Name: filepath.Base(path), Config: path, Valid: true})
		}
		return results
	}
	t.Cleanup(func() {
		firewallApplierInstance = originalFirewall
		serviceActionRunner = originalRunner
		serviceHealthChecker = originalHealth
		stagedConfigValidator = originalValidator
	})

	root := t.TempDir()
	state := newManagementState(ServerInfo{
		Version: "test", Mode: "dev",
		StatePath: filepath.Join(root, "state.json"),
		KeyPath:   filepath.Join(root, "state.key"),
		ApplyRoot: root,
	})
	state.mu.Lock()
	err := state.saveLocked()
	state.mu.Unlock()
	if err != nil {
		t.Fatalf("persist initial management state: %v", err)
	}
	t.Cleanup(func() {
		if err := closeClientSubsystem(state); err != nil {
			t.Errorf("close client subsystem: %v", err)
		}
	})
	if state.trafficCollector == nil || state.trafficReconciler == nil {
		t.Fatal("client background workers were not initialized")
	}
	return state
}

func TestRepeatedReloadsRetainExactlyOneTrafficWorkerPair(t *testing.T) {
	state := newClientLifecycleTestState(t)
	collector := state.trafficCollector
	reconciler := state.trafficReconciler
	for i := 0; i < 5; i++ {
		if err := state.Reload(); err != nil {
			t.Fatalf("reload %d: %v", i+1, err)
		}
		if state.trafficCollector != collector {
			t.Fatalf("reload %d replaced the live collector", i+1)
		}
		if state.trafficReconciler != reconciler {
			t.Fatalf("reload %d replaced the live reconciler", i+1)
		}
	}
}

func TestManagementStateCloseStopsEveryTrafficWorker(t *testing.T) {
	state := newClientLifecycleTestState(t)
	collector := state.trafficCollector
	reconciler := state.trafficReconciler
	if !collector.Running() || !reconciler.Running() {
		t.Fatal("precondition: workers are not running")
	}
	if err := state.Close(); err != nil {
		t.Fatal(err)
	}
	if collector.Running() || reconciler.Running() {
		t.Fatal("shutdown returned before every worker stopped")
	}
	if state.trafficCollector != nil || state.trafficReconciler != nil || state.db != nil {
		t.Fatal("shutdown retained worker or database ownership")
	}
}

type lifecycleTrafficProvider struct {
	readings map[string]client.ProviderReading
}

func (*lifecycleTrafficProvider) Key() string { return "lifecycle-test" }
func (p *lifecycleTrafficProvider) Read() (map[string]client.ProviderReading, error) {
	return p.readings, nil
}

func TestTwoCollectionIntervalsAfterReloadRecordOneMonotonicDelta(t *testing.T) {
	state := newClientLifecycleTestState(t)
	row, err := state.clientRepo.Create(client.Client{Name: "reload-traffic", Enabled: true, QuotaResetPolicy: client.ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := state.clientRepo.CreateBinding(client.Binding{ClientID: row.ID, InboundID: "lifecycle", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	provider := &lifecycleTrafficProvider{readings: map[string]client.ProviderReading{
		binding.ID: {BindingID: binding.ID, UploadBytes: 100, DownloadBytes: 100},
	}}
	collector := state.trafficCollector
	collector.ResetProviders([]client.TrafficProvider{provider})
	if err := collector.CollectOnce(); err != nil {
		t.Fatalf("baseline collect: %v", err)
	}

	if err := state.Reload(); err != nil {
		t.Fatal(err)
	}
	if state.trafficCollector != collector {
		t.Fatal("reload created a second collector")
	}
	collector.ResetProviders([]client.TrafficProvider{provider})
	provider.readings[binding.ID] = client.ProviderReading{BindingID: binding.ID, UploadBytes: 200, DownloadBytes: 200}
	if err := collector.CollectOnce(); err != nil {
		t.Fatalf("first interval: %v", err)
	}
	provider.readings[binding.ID] = client.ProviderReading{BindingID: binding.ID, UploadBytes: 300, DownloadBytes: 300}
	if err := collector.CollectOnce(); err != nil {
		t.Fatalf("second interval: %v", err)
	}
	up, down, err := state.trafficStore.TotalsForClient(row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if up != 200 || down != 200 {
		t.Fatalf("two post-reload intervals totals=%d/%d, want 200/200", up, down)
	}
}

func TestQuotaCrossingAfterReloadCreatesOneTransitionRevisionAndJob(t *testing.T) {
	state := newClientLifecycleTestState(t)
	quota := int64(100)
	row, err := state.clientRepo.Create(client.Client{
		Name: "reload-quota", Enabled: true, QuotaBytes: &quota, QuotaResetPolicy: client.ResetNever,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := state.trafficStore.RecordSample(client.Sample{ClientID: row.ID, UploadBytes: 101, AtUnix: 1}); err != nil {
		t.Fatal(err)
	}
	beforeRevision, err := state.applyRevisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	beforeJobs, err := state.applyJobs.List(100)
	if err != nil {
		t.Fatal(err)
	}
	reconciler := state.trafficReconciler

	if err := state.Reload(); err != nil {
		t.Fatal(err)
	}
	if state.trafficReconciler != reconciler {
		t.Fatal("reload created a second reconciler")
	}
	changed, err := reconciler.ReconcileOnce()
	if err != nil {
		t.Fatal(err)
	}
	if changed != 1 {
		t.Fatalf("depleted transitions=%d, want 1", changed)
	}
	afterRevision, err := state.applyRevisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	afterJobs, err := state.applyJobs.List(100)
	if err != nil {
		t.Fatal(err)
	}
	if afterRevision.Desired != beforeRevision.Desired+1 {
		t.Fatalf("desired revision advanced %d -> %d, want exactly one", beforeRevision.Desired, afterRevision.Desired)
	}
	if len(afterJobs) != len(beforeJobs)+1 {
		t.Fatalf("apply jobs advanced %d -> %d, want exactly one", len(beforeJobs), len(afterJobs))
	}
	updated, err := state.clientRepo.Get(row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Depleted {
		t.Fatal("quota crossing did not persist depleted transition")
	}

	changed, err = reconciler.ReconcileOnce()
	if err != nil {
		t.Fatal(err)
	}
	if changed != 0 {
		t.Fatalf("second reconcile repeated %d transition(s)", changed)
	}
	finalRevision, _ := state.applyRevisions.Get()
	finalJobs, _ := state.applyJobs.List(100)
	if finalRevision.Desired != afterRevision.Desired || len(finalJobs) != len(afterJobs) {
		t.Fatal("stable depleted state created another revision or apply job")
	}
}
