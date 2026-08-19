package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestClientRequestGateExcludesRestoreWriterUntilRequestCompletes(t *testing.T) {
	state := &managementState{}
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	handler := clientRequestGateMiddleware(state, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(entered)
		<-release
	}))
	go func() {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/clients", nil))
		close(done)
	}()
	<-entered
	writerAcquired := make(chan struct{})
	go func() {
		state.clientRequestMu.Lock()
		close(writerAcquired)
		state.clientRequestMu.Unlock()
	}()
	select {
	case <-writerAcquired:
		t.Fatal("restore writer entered while DB-backed request was active")
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("request did not finish")
	}
	select {
	case <-writerAcquired:
	case <-time.After(time.Second):
		t.Fatal("restore writer did not proceed after request")
	}
}

func TestTrackedAutoApplyUsesSnapshotIsolationOutsideGlobalMutex(t *testing.T) {
	body, err := os.ReadFile("management_operational_routes.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(body)
	if !strings.Contains(source, "s.mu.Unlock()\n		defer s.mu.Lock()") {
		t.Fatal("tracked apply does not release/reacquire global mutex around runner")
	}
	if !strings.Contains(source, "runner.RunLatest") && !strings.Contains(source, "runner.RunContext") {
		t.Fatal("tracked apply does not invoke runner outside the global mutex")
	}
	body, err = os.ReadFile("apply_subsystem.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "newApplyExecutionStateLocked(snapshot)") {
		t.Fatal("apply runner does not execute against an isolated revision snapshot")
	}
}
