package api

import "testing"

func TestApplyTrackedRouterHelperClosesLifecycleAtCleanup(t *testing.T) {
	var state *managementState
	t.Run("construct", func(t *testing.T) {
		_, state = newApplyTrackedRouterWithState(t)
		if state.trafficCollector == nil || state.trafficReconciler == nil {
			t.Fatal("precondition: helper did not start client background workers")
		}
	})
	if state == nil {
		t.Fatal("helper did not return management state")
	}
	// Keep the RED test hermetic before the helper owns this cleanup.
	defer func() { _ = state.Close() }()

	state.mu.Lock()
	collector := state.trafficCollector
	reconciler := state.trafficReconciler
	state.mu.Unlock()
	if collector != nil || reconciler != nil || state.db != nil {
		t.Fatalf("helper cleanup leaked lifecycle resources: collector=%v reconciler=%v dbOpen=%t", collector != nil, reconciler != nil, state.db != nil)
	}
}
