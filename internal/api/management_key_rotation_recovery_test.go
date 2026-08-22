package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/privileged"
)

type orderedRecoveryClient struct {
	*recordingPrivilegedClient
	mu       sync.Mutex
	sequence []string
}

func (c *orderedRecoveryClient) RotateKey(context.Context, privileged.RotateKeyRequest) error {
	c.mu.Lock()
	c.sequence = append(c.sequence, "rotate")
	c.mu.Unlock()
	return errors.New("simulated helper interruption")
}

func (c *orderedRecoveryClient) RecoverKeyRotation(context.Context, privileged.RecoverKeyRotationRequest) error {
	c.mu.Lock()
	c.sequence = append(c.sequence, "recover")
	c.mu.Unlock()
	return nil
}

func (c *orderedRecoveryClient) reset() {
	c.mu.Lock()
	c.sequence = nil
	c.mu.Unlock()
}

func (c *orderedRecoveryClient) calls() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.sequence...)
}

func TestRotateKeyErrorRecoversThroughHelperBeforeReload(t *testing.T) {
	root := t.TempDir()
	client := &orderedRecoveryClient{recordingPrivilegedClient: &recordingPrivilegedClient{}}
	state := newManagementState(ServerInfo{
		Version: "test", Mode: "dev",
		StatePath:               filepath.Join(root, "state.json"),
		KeyPath:                 filepath.Join(root, "state.key"),
		ApplyRoot:               root,
		Privileged:              client,
		RequirePrivilegedHelper: true,
	})
	defer closeClientSubsystem(state)
	collectorBefore := state.trafficCollector
	reconcilerBefore := state.trafficReconciler
	client.reset() // Ignore the required startup recovery call.

	admin, err := state.sessionRegistry().Create(SessionCreateInput{Username: "admin", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	request := adminJSONRequest(http.MethodPost, "/api/admin/rotate-key", `{}`)
	request.AddCookie(&http.Cookie{Name: "veil_session", Value: admin.Token})
	response := httptest.NewRecorder()
	state.handleRotateKey(response, request)
	if response.Code == http.StatusOK {
		t.Fatalf("interrupted rotation returned success: %s", response.Body.String())
	}
	calls := client.calls()
	if len(calls) < 2 || calls[0] != "rotate" || calls[1] != "recover" {
		t.Fatalf("privileged call order=%v, want rotate then recover before reload", calls)
	}
	if state.startupStateLoadFailed {
		t.Fatal("successful privileged recovery and reload left Panel failed closed")
	}
	if state.trafficCollector != collectorBefore || state.trafficReconciler != reconcilerBefore {
		t.Fatal("interrupted key rotation leaked or replaced traffic workers")
	}
}
