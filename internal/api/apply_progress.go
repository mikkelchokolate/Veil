package api

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mikkelchokolate/Veil/internal/privileged"
)

type applyProgressView struct {
	Stage     string `json:"stage"`
	Runtime   string `json:"runtime,omitempty"`
	StartedAt int64  `json:"startedAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

type applyProgressTracker struct {
	mu          sync.Mutex
	operationID string
	progress    *applyProgressView
}

func (p *applyProgressTracker) snapshot() *applyProgressView {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.progress == nil {
		return nil
	}
	value := *p.progress
	return &value
}

func (ctx ManagementApplyContext) reportProgress(stage, runtime string) {
	p := ctx.state.applyProgress
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.progress == nil || p.operationID != ctx.fenceToken().OperationID {
		return
	}
	p.progress.Stage, p.progress.Runtime, p.progress.UpdatedAt = stage, runtime, time.Now().Unix()
}

func (ctx ManagementApplyContext) beginProgress() {
	p := ctx.state.applyProgress
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.operationID = ctx.fenceToken().OperationID
	now := time.Now().Unix()
	p.progress = &applyProgressView{Stage: "installing_runtime", StartedAt: now, UpdatedAt: now}
}

func (ctx ManagementApplyContext) clearProgress() {
	p := ctx.state.applyProgress
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.operationID == ctx.fenceToken().OperationID {
		p.progress = nil
	}
}

func (ctx ManagementApplyContext) observeRuntimeProgress() func() {
	observer, ok := ctx.state.privileged.(privileged.RuntimeInstallObserver)
	if !ok {
		return func() {}
	}
	operationID := ctx.fenceToken().OperationID
	watchCtx, cancel := context.WithCancel(ctx.operationContext())
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-watchCtx.Done():
				return
			case <-ticker.C:
				readCtx, stop := context.WithTimeout(watchCtx, 2*time.Second)
				progress, err := observer.RuntimeInstallStatus(readCtx, privileged.RuntimeInstallStatusRequest{OperationID: operationID})
				stop()
				if watchCtx.Err() != nil {
					return
				}
				if err == nil && progress.OperationID == operationID && progress.Stage != "" {
					p := ctx.state.applyProgress
					if p != nil {
						p.mu.Lock()
						if p.progress != nil && p.operationID == operationID {
							p.progress.Stage, p.progress.Runtime, p.progress.UpdatedAt = progress.Stage, progress.Runtime, progress.UpdatedAt
						}
						p.mu.Unlock()
					}
				}
			}
		}
	}()
	return func() { cancel(); <-done }
}

// Only authenticated mutations that can apply runtimes get the build budget.
func allowRuntimeMutationTime(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
		return
	}
	for _, path := range []string{"/api/inbounds", "/api/warp", "/api/settings", "/api/apply", "/api/v1/clients", "/api/routing"} {
		if r.URL.Path == path || strings.HasPrefix(r.URL.Path, path+"/") {
			_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(17 * time.Minute))
			return
		}
	}
}
