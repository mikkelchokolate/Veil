package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/apply"
	"github.com/mikkelchokolate/Veil/internal/privileged"
	"github.com/mikkelchokolate/Veil/internal/statecommit"
)

type rotatingPrivilegedClient struct {
	*recordingPrivilegedClient
	statePath string
	keyPath   string
}

func (c *rotatingPrivilegedClient) RotateKey(context.Context, privileged.RotateKeyRequest) error {
	c.rotateCalls++
	_, err := statecommit.RotateKey(statecommit.RotateKeyOptions{
		StatePath: c.statePath, KeyPath: c.keyPath, TargetKeyPath: c.keyPath,
	})
	return err
}

func TestRotateKeyAutoAppliesPendingRevision(t *testing.T) {
	origValidator := stagedConfigValidator
	origRunner := serviceActionRunner
	origHealth := serviceHealthChecker
	origAutoApply := autoApplyAfterMutation
	origFirewall := currentFirewallApplier()
	t.Cleanup(func() {
		stagedConfigValidator = origValidator
		serviceActionRunner = origRunner
		serviceHealthChecker = origHealth
		autoApplyAfterMutation = origAutoApply
		swapFirewallApplier(origFirewall)
	})
	stagedConfigValidator = func(paths []string) []ConfigValidationResult {
		out := make([]ConfigValidationResult, 0, len(paths))
		for _, p := range paths {
			out = append(out, ConfigValidationResult{Name: p, Config: p, Valid: true})
		}
		return out
	}
	serviceActionRunner = func(command []string) ServiceActionResult {
		return ServiceActionResult{Command: command, Success: true}
	}
	serviceHealthChecker = func(serviceName string) ServiceHealthResult {
		return ServiceHealthResult{Name: serviceName, Healthy: true}
	}
	swapFirewallApplier(&fakeFirewallApplier{})
	autoApplyAfterMutation = true

	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	keyPath := filepath.Join(root, "state.key")
	privilegedClient := &rotatingPrivilegedClient{
		recordingPrivilegedClient: &recordingPrivilegedClient{},
		statePath:                 statePath,
		keyPath:                   keyPath,
	}
	state := newManagementState(ServerInfo{
		Version: "test", Mode: "dev",
		StatePath: statePath, KeyPath: keyPath,
		ApplyRoot: root, Privileged: privilegedClient,
	})
	defer closeClientSubsystem(state)

	initial := httptest.NewRecorder()
	state.handleSettings(initial, httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(`{
		"panelListen":"127.0.0.1:2096","mode":"dev","domain":"before.example.com",
		"firewallManagement":false
	}`)))
	if initial.Code != http.StatusOK {
		t.Fatalf("initial settings status=%d body=%s", initial.Code, initial.Body.String())
	}
	before, err := state.applyRevisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	if before.Desired == 0 || before.Desired != before.Applied {
		t.Fatalf("pre-rotate revisions not synced: %+v body=%s", before, initial.Body.String())
	}

	admin, err := state.sessionRegistry().Create(SessionCreateInput{Username: "admin", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	request := adminJSONRequest(http.MethodPost, "/api/admin/rotate-key", `{}`)
	request.AddCookie(&http.Cookie{Name: "veil_session", Value: admin.Token})
	response := httptest.NewRecorder()
	state.handleRotateKey(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("rotate status=%d body=%s", response.Code, response.Body.String())
	}

	var body struct {
		Success         bool         `json:"success"`
		RevokedSessions int          `json:"revokedSessions"`
		Revision        revisionView `json:"revision"`
		ApplyJob        *apply.Job   `json:"applyJob"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode rotate response: %v body=%s", err, response.Body.String())
	}
	if !body.Success {
		t.Fatalf("rotate apply reported failure: %s", response.Body.String())
	}
	if body.ApplyJob == nil || body.ApplyJob.ID == "" {
		t.Fatalf("rotate response missing apply job: %s", response.Body.String())
	}
	if body.ApplyJob.Trigger != "mutation" {
		t.Fatalf("apply trigger=%q want mutation", body.ApplyJob.Trigger)
	}
	if body.Revision.Desired <= before.Desired {
		t.Fatalf("desired did not advance: before=%+v after=%+v", before, body.Revision)
	}
	if body.Revision.Applied != body.Revision.Desired || body.Revision.State != apply.StateSynced {
		t.Fatalf("rotate left apply pending: %+v job=%+v", body.Revision, body.ApplyJob)
	}

	after, err := state.applyRevisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	if after.Desired != after.Applied || after.Desired != body.Revision.Desired {
		t.Fatalf("stored revisions drifted from envelope: stored=%+v envelope=%+v", after, body.Revision)
	}
}
