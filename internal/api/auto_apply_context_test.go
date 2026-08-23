package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	veilapply "github.com/mikkelchokolate/Veil/internal/apply"
)

func TestAutoApplyAfterMutationIgnoresCanceledRequest(t *testing.T) {
	_, state := newApplyTrackedRouterWithState(t)

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodPost, "/api/inbounds", nil).WithContext(canceled)

	called := false
	var execErr error
	state.applyRunner.Close()
	state.applyRunner = veilapply.NewRunner(state.applyRevisions, state.applyJobs, veilapply.ContextExecutorFunc(func(ctx context.Context, _ uint64) (veilapply.Result, error) {
		called = true
		execErr = ctx.Err()
		if execErr != nil {
			return veilapply.Result{}, execErr
		}
		return veilapply.Result{
			Success:          true,
			Disposition:      veilapply.ApplyDispositionRuntimeConverged,
			MarkRevisionLive: true,
		}, nil
	}))

	state.mu.Lock()
	if _, err := state.bumpDesiredRevisionLocked(); err != nil {
		state.mu.Unlock()
		t.Fatalf("bump desired revision: %v", err)
	}
	outcome := state.autoApplyResultLocked(req, "test")
	state.mu.Unlock()

	if !called {
		t.Fatal("mutation apply returned before the executor; apply used the canceled request context")
	}
	if execErr != nil {
		t.Fatalf("mutation apply context was canceled during execute: %v", execErr)
	}
	if outcome.job != nil && errors.Is(errors.New(outcome.job.ErrorMessage), context.Canceled) {
		t.Fatalf("apply job recorded the canceled request: %+v", outcome.job)
	}
}
