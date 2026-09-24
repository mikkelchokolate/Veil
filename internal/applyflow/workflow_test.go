package applyflow

import (
	"errors"
	"net/http"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

// fakeState is a configurable State double covering every RunLocked branch.
type fakeState struct {
	plan model.ApplyPlanResponse

	wrote       bool
	written     []string
	validations []model.ConfigValidationResult
	rendered    []string
	writeErr    error
	promoted    []string

	liveFiles   []string
	backupFiles []string
	promotions  []PromotionRecord
	promoteErr  error

	serviceActions []model.ServiceActionResult

	rollbackFiles   []string
	removedFiles    []string
	rollbackActions []model.ServiceActionResult
	rolledBack      bool

	history []string
}

func (s *fakeState) BuildApplyPlanLocked() model.ApplyPlanResponse { return s.plan }
func (s *fakeState) WriteApplyStageLocked(model.ApplyPlanResponse) ([]string, []model.ConfigValidationResult, []string, error) {
	s.wrote = true
	return s.written, s.validations, s.rendered, s.writeErr
}
func (s *fakeState) PromoteStagedConfigsLocked(paths []string) ([]string, []string, []PromotionRecord, error) {
	s.promoted = append([]string(nil), paths...)
	return s.liveFiles, s.backupFiles, s.promotions, s.promoteErr
}
func (s *fakeState) ReloadPromotedServicesLocked([]string) []model.ServiceActionResult {
	return s.serviceActions
}
func (s *fakeState) RollbackPromotedConfigsLocked([]PromotionRecord, []string) ([]string, []string, []model.ServiceActionResult) {
	s.rolledBack = true
	return s.rollbackFiles, s.removedFiles, s.rollbackActions
}
func (s *fakeState) AppendApplyHistoryLocked(stage string, _ bool, _ model.ApplyResponse) error {
	s.history = append(s.history, stage)
	return nil
}

func healthAllHealthy(actions []model.ServiceActionResult) []model.ServiceHealthResult {
	out := make([]model.ServiceHealthResult, 0, len(actions))
	for _, a := range actions {
		out = append(out, model.ServiceHealthResult{Name: a.Name, Healthy: true})
	}
	return out
}

func TestWorkflowRequiresConfirmBeforeWritingStage(t *testing.T) {
	state := &fakeState{plan: model.ApplyPlanResponse{Valid: true}}
	response, status, err := NewWorkflow(state, nil).RunLocked(model.ApplyRequest{})
	if err == nil || status != http.StatusBadRequest || response.Applied {
		t.Fatalf("response=%+v status=%d err=%v", response, status, err)
	}
	if state.wrote {
		t.Fatalf("workflow wrote staged files without confirm")
	}
}

func TestWorkflowRejectsInvalidPlan(t *testing.T) {
	state := &fakeState{plan: model.ApplyPlanResponse{Valid: false}}
	resp, status, err := NewWorkflow(state, nil).RunLocked(model.ApplyRequest{Confirm: true})
	if status != http.StatusBadRequest || err != nil || resp.Applied {
		t.Fatalf("invalid plan must 400 without applying: resp=%+v status=%d err=%v", resp, status, err)
	}
	if state.wrote {
		t.Fatal("invalid plan must not write stage")
	}
}

func TestWorkflowRejectsServicesWithoutLive(t *testing.T) {
	state := &fakeState{plan: model.ApplyPlanResponse{Valid: true}}
	resp, status, _ := NewWorkflow(state, nil).RunLocked(model.ApplyRequest{Confirm: true, ApplyServices: true})
	if status != http.StatusBadRequest || resp.Applied {
		t.Fatalf("applyServices without applyLive must 400: resp=%+v status=%d", resp, status)
	}
}

func TestWorkflowStagesOnlyWithConfirm(t *testing.T) {
	state := &fakeState{plan: model.ApplyPlanResponse{Valid: true}, written: []string{"a.json"}}
	resp, status, err := NewWorkflow(state, nil).RunLocked(model.ApplyRequest{Confirm: true})
	if err != nil || status != http.StatusOK {
		t.Fatalf("stage-only should succeed: status=%d err=%v", status, err)
	}
	if resp.Applied || resp.LiveApplied || resp.ServicesApplied {
		t.Fatalf("stage-only flags wrong: %+v", resp)
	}
	if len(state.history) != 1 || state.history[0] != "staged" {
		t.Fatalf("history = %v, want [staged]", state.history)
	}
}

// A configured validator that could not run (PATH miss → Skipped) must block
// live promotion — otherwise bad config promotes whenever the checker binary
// is absent (issue #686). Protocols with no standalone checker produce no
// validation entry at all, so this gate does not affect them.
func TestWorkflowSkippedValidationBlocksLiveApply(t *testing.T) {
	state := &fakeState{
		plan: model.ApplyPlanResponse{Valid: true},
		validations: []model.ConfigValidationResult{{
			Name:    "caddy",
			Skipped: true,
			Error:   "caddy not found; syntax validation skipped",
		}},
		liveFiles: []string{"/live/caddy/config.json"},
	}
	resp, status, err := NewWorkflow(state, nil).RunLocked(model.ApplyRequest{Confirm: true, ApplyLive: true})
	if err != nil || status != http.StatusBadRequest {
		t.Fatalf("skipped validation must block live apply with 400: status=%d err=%v", status, err)
	}
	if resp.LiveApplied || len(state.promoted) != 0 {
		t.Fatalf("nothing may promote when a configured validator is missing: %+v promoted=%v", resp, state.promoted)
	}
	if len(state.history) == 0 || state.history[len(state.history)-1] != "validation" {
		t.Fatalf("history = %v, want last 'validation'", state.history)
	}
}

func TestWorkflowPromotesWrittenFilesIncludingRoutingDat(t *testing.T) {
	written := []string{
		"/stage/generated/veil/apply-plan.json",
		"/stage/generated/hysteria2/edge.yaml",
		"/stage/generated/rules/geosite.dat",
	}
	state := &fakeState{
		plan:      model.ApplyPlanResponse{Valid: true},
		written:   written,
		rendered:  []string{"/stage/generated/hysteria2/edge.yaml"},
		liveFiles: []string{"/live/hysteria2/edge.yaml"},
	}
	_, status, err := NewWorkflow(state, nil).RunLocked(model.ApplyRequest{Confirm: true, ApplyLive: true})
	if err != nil || status != http.StatusOK {
		t.Fatalf("status=%d err=%v", status, err)
	}
	if len(state.promoted) != len(written) {
		t.Fatalf("promoted=%q, want written files including geosite.dat", state.promoted)
	}
	got := map[string]bool{}
	for _, path := range state.promoted {
		got[path] = true
	}
	if !got["/stage/generated/rules/geosite.dat"] {
		t.Fatalf("geosite.dat was not promoted: %q", state.promoted)
	}
}

func TestWorkflowBlocksLiveApplyOnFailedValidation(t *testing.T) {
	state := &fakeState{
		plan:        model.ApplyPlanResponse{Valid: true},
		validations: []model.ConfigValidationResult{{Name: "caddy", Valid: false, Error: "bad config"}},
	}
	resp, status, err := NewWorkflow(state, nil).RunLocked(model.ApplyRequest{Confirm: true, ApplyLive: true})
	if status != http.StatusBadRequest || err != nil {
		t.Fatalf("failed validation must 400: status=%d err=%v", status, err)
	}
	if resp.LiveApplied {
		t.Fatal("must not promote when validation fails")
	}
	if len(state.history) == 0 || state.history[len(state.history)-1] != "validation" {
		t.Fatalf("history = %v, want last 'validation'", state.history)
	}
}

func TestWorkflowAppliesServicesAndPassesHealth(t *testing.T) {
	state := &fakeState{
		plan:           model.ApplyPlanResponse{Valid: true},
		validations:    []model.ConfigValidationResult{{Name: "mieru", Valid: true}},
		liveFiles:      []string{"/live/mieru/server_config.json"},
		serviceActions: []model.ServiceActionResult{{Name: "veil-mieru.service", Success: true}},
	}
	resp, status, err := NewWorkflow(state, healthAllHealthy).RunLocked(model.ApplyRequest{Confirm: true, ApplyLive: true, ApplyServices: true})
	if err != nil || status != http.StatusOK {
		t.Fatalf("full apply should succeed: status=%d err=%v", status, err)
	}
	if !resp.Applied || !resp.LiveApplied || !resp.ServicesApplied || resp.RolledBack {
		t.Fatalf("full apply flags wrong: %+v", resp)
	}
	if len(resp.HealthChecks) != 1 || !resp.HealthChecks[0].Healthy {
		t.Fatalf("health checks = %+v", resp.HealthChecks)
	}
	if state.history[len(state.history)-1] != "services" {
		t.Fatalf("history = %v, want last 'services'", state.history)
	}
}

func TestWorkflowRollsBackOnServiceActionFailure(t *testing.T) {
	state := &fakeState{
		plan:            model.ApplyPlanResponse{Valid: true},
		liveFiles:       []string{"/live/x"},
		serviceActions:  []model.ServiceActionResult{{Name: "veil-mieru.service", Success: false, Error: "start failed"}},
		rollbackFiles:   []string{"/live/x"},
		rollbackActions: []model.ServiceActionResult{{Name: "veil-mieru.service", Success: true}},
	}
	resp, status, _ := NewWorkflow(state, healthAllHealthy).RunLocked(model.ApplyRequest{Confirm: true, ApplyLive: true, ApplyServices: true})
	if status != http.StatusBadRequest {
		t.Fatalf("service failure must 400, got %d", status)
	}
	if !resp.RolledBack || !state.rolledBack {
		t.Fatalf("expected rollback, got %+v", resp)
	}
	if resp.ServicesApplied {
		t.Fatal("ServicesApplied must be false on rollback")
	}
	// A clean self-rollback proves the triad: mutation started, the rollback
	// completed, and nothing is ambiguous — the job is terminally failed, not
	// recovery_pending (#888).
	if !resp.MutationStarted || !resp.RollbackComplete || resp.Ambiguous {
		t.Fatalf("clean rollback triad wrong: %+v", resp)
	}
	if len(state.history) == 0 || state.history[len(state.history)-1] != "rollback" {
		t.Fatalf("clean rollback history = %v, want last 'rollback'", state.history)
	}
}

func TestWorkflowRollsBackOnHealthFailure(t *testing.T) {
	unhealthy := func([]model.ServiceActionResult) []model.ServiceHealthResult {
		return []model.ServiceHealthResult{{Name: "veil-mieru.service", Healthy: false, Error: "down"}}
	}
	state := &fakeState{
		plan:           model.ApplyPlanResponse{Valid: true},
		liveFiles:      []string{"/live/x"},
		serviceActions: []model.ServiceActionResult{{Name: "veil-mieru.service", Success: true}},
		rollbackFiles:  []string{"/live/x"},
	}
	resp, status, _ := NewWorkflow(state, unhealthy).RunLocked(model.ApplyRequest{Confirm: true, ApplyLive: true, ApplyServices: true})
	if status != http.StatusBadRequest {
		t.Fatalf("health failure must 400, got %d", status)
	}
	if resp.RolledBack || !resp.Ambiguous {
		t.Fatalf("unhealthy rollback must remain recovery-pending, got %+v", resp)
	}
	if len(state.history) == 0 || state.history[len(state.history)-1] != "ambiguous" {
		t.Fatalf("unhealthy rollback must record 'ambiguous' history, got %v", state.history)
	}
}

// Regression for #343: a first-create config has no previous generation to
// restore — the rollback deletes it instead. A successful deletion plus
// stop/disable must complete the rollback (terminal failed apply), not strand
// the job as recovery_pending.
func TestWorkflowCompletesRollbackWhenNewConfigIsDeleted(t *testing.T) {
	calls := 0
	health := func([]model.ServiceActionResult) []model.ServiceHealthResult {
		calls++
		// First call covers the failed candidate services; the second covers
		// the restored state, which is healthy again.
		return []model.ServiceHealthResult{{Name: "veil-hysteria2@edge.service", Healthy: calls > 1}}
	}
	state := &fakeState{
		plan:           model.ApplyPlanResponse{Valid: true},
		liveFiles:      []string{"/live/hysteria2/edge.yaml"},
		serviceActions: []model.ServiceActionResult{{Name: "veil-hysteria2@edge.service", Success: true}},
		removedFiles:   []string{"/live/hysteria2/edge.yaml"},
		rollbackActions: []model.ServiceActionResult{
			{Name: "veil-hysteria2@edge.service", Success: true},
			{Name: "veil-hysteria2@edge.service", Success: true},
		},
	}
	resp, status, _ := NewWorkflow(state, health).RunLocked(model.ApplyRequest{Confirm: true, ApplyLive: true, ApplyServices: true})
	if status != http.StatusBadRequest {
		t.Fatalf("health failure must 400, got %d", status)
	}
	if !resp.ArtifactsRestored || !resp.RollbackComplete || !resp.RolledBack || resp.Ambiguous {
		t.Fatalf("successful first-create rollback must complete: %+v", resp)
	}
}

func TestWorkflowLeavesRecoveryPendingWhenRestoredServiceActionFails(t *testing.T) {
	state := &fakeState{
		plan:            model.ApplyPlanResponse{Valid: true},
		liveFiles:       []string{"/live/x"},
		serviceActions:  []model.ServiceActionResult{{Name: "veil-mieru.service", Success: false, Error: "reload failed"}},
		rollbackFiles:   []string{"/live/x"},
		rollbackActions: []model.ServiceActionResult{{Name: "veil-mieru.service", Success: false, Error: "restart previous generation failed"}},
	}
	resp, status, _ := NewWorkflow(state, healthAllHealthy).RunLocked(model.ApplyRequest{Confirm: true, ApplyLive: true, ApplyServices: true})
	if status != http.StatusBadRequest {
		t.Fatalf("status=%d response=%+v", status, resp)
	}
	if resp.RolledBack || resp.RollbackComplete || !resp.Ambiguous {
		t.Fatalf("failed service restoration was reported complete: %+v", resp)
	}
	if len(state.history) == 0 || state.history[len(state.history)-1] != "ambiguous" {
		t.Fatalf("incomplete rollback must record 'ambiguous' history, got %v", state.history)
	}
}

func TestWorkflowLeavesRecoveryPendingWhenRestoredServiceUnhealthy(t *testing.T) {
	checks := 0
	health := func([]model.ServiceActionResult) []model.ServiceHealthResult {
		checks++
		return []model.ServiceHealthResult{{Name: "veil-mieru.service", Healthy: false, Error: "restored generation unhealthy"}}
	}
	state := &fakeState{
		plan:            model.ApplyPlanResponse{Valid: true},
		liveFiles:       []string{"/live/x"},
		serviceActions:  []model.ServiceActionResult{{Name: "veil-mieru.service", Success: true}},
		rollbackFiles:   []string{"/live/x"},
		rollbackActions: []model.ServiceActionResult{{Name: "veil-mieru.service", Success: true}},
	}
	resp, status, _ := NewWorkflow(state, health).RunLocked(model.ApplyRequest{Confirm: true, ApplyLive: true, ApplyServices: true})
	if status != http.StatusBadRequest || checks != 2 {
		t.Fatalf("status=%d checks=%d response=%+v", status, checks, resp)
	}
	if resp.RolledBack || resp.PostRollbackHealthPass || resp.RollbackComplete || !resp.Ambiguous {
		t.Fatalf("unhealthy restored service was reported complete: %+v", resp)
	}
	if len(state.history) == 0 || state.history[len(state.history)-1] != "ambiguous" {
		t.Fatalf("unhealthy restored service must record 'ambiguous' history, got %v", state.history)
	}
}

func TestWorkflowReturns500OnStageWriteError(t *testing.T) {
	state := &fakeState{plan: model.ApplyPlanResponse{Valid: true}, writeErr: errors.New("disk full")}
	resp, status, err := NewWorkflow(state, nil).RunLocked(model.ApplyRequest{Confirm: true})
	if status != http.StatusInternalServerError || err == nil {
		t.Fatalf("stage write error must 500: status=%d err=%v", status, err)
	}
	// Nothing was staged: no mutation evidence may be claimed and no history
	// entry is written — there is no apply outcome to record.
	if resp.MutationStarted || resp.Ambiguous || resp.LiveApplied || resp.Applied {
		t.Fatalf("stage-write failure must not claim mutation evidence: %+v", resp)
	}
	if len(state.history) != 0 {
		t.Fatalf("stage-write failure must not write history: %v", state.history)
	}
}

func TestWorkflowReturns500OnPromoteError(t *testing.T) {
	state := &fakeState{plan: model.ApplyPlanResponse{Valid: true}, promoteErr: errors.New("helper down")}
	resp, status, err := NewWorkflow(state, nil).RunLocked(model.ApplyRequest{Confirm: true, ApplyLive: true})
	if status != http.StatusInternalServerError || err == nil {
		t.Fatalf("promote error must 500: status=%d err=%v", status, err)
	}
	// An unmarked promote failure cannot prove the live tree is untouched: the
	// triad says mutation may have started and the outcome stays ambiguous, so
	// the durable layer strands it as recovery_pending rather than claiming a
	// clean failure (#888).
	if !resp.MutationStarted || !resp.Ambiguous {
		t.Fatalf("unmarked promote failure must report MutationStarted+Ambiguous: %+v", resp)
	}
	if len(state.history) == 0 || state.history[len(state.history)-1] != "ambiguous" {
		t.Fatalf("ambiguous promote failure must record 'ambiguous' history: %v", state.history)
	}
}

// #967: a promote failure that provably happened before any live mutation
// (path escape, orphan scan, digest build, missing helper) is a clean failure
// — MutationStarted/Ambiguous stay false so the durable runner finalizes the
// job as failed instead of recovery_pending.
func TestWorkflowPromotePreMutationErrorIsNotAmbiguous(t *testing.T) {
	state := &fakeState{plan: model.ApplyPlanResponse{Valid: true},
		promoteErr: MarkPreMutationError(errors.New("privileged helper is unavailable"))}
	resp, status, err := NewWorkflow(state, nil).RunLocked(model.ApplyRequest{Confirm: true, ApplyLive: true})
	if status != http.StatusInternalServerError || err == nil {
		t.Fatalf("pre-mutation promote error must 500: status=%d err=%v", status, err)
	}
	var preMutation *PreMutationError
	if !errors.As(err, &preMutation) {
		t.Fatalf("error must keep the PreMutationError marker: %v", err)
	}
	if resp.MutationStarted || resp.Ambiguous {
		t.Fatalf("pre-mutation promote failure must not claim mutation or ambiguity: %+v", resp)
	}
	if resp.LiveApplied || resp.Applied {
		t.Fatalf("pre-mutation promote failure must not claim progress: %+v", resp)
	}
	// The apply was staged before the promote ran — that is the honest stage.
	if len(state.history) != 1 || state.history[0] != "staged" {
		t.Fatalf("pre-mutation failure history = %v, want [staged]", state.history)
	}
}

// #542: when the plan declares service work (reload_service) but no service
// action ran, promotion alone is not runtime convergence — Applied must stay
// false so the durable layer keeps desired>applied instead of marking the
// revision live.
func TestWorkflowAppliedRequiresServiceEvidence(t *testing.T) {
	state := &fakeState{
		plan: model.ApplyPlanResponse{
			Valid: true,
			Operations: []model.ApplyOperation{
				{Type: "promote_file", Source: "/g/x", Destination: "/live/x"},
				{Type: "reload_service", Unit: "veil-x.service"},
			},
		},
		liveFiles:      []string{"/live/x"},
		serviceActions: nil, // plan expected a reload, none ran
	}
	resp, status, err := NewWorkflow(state, healthAllHealthy).RunLocked(model.ApplyRequest{Confirm: true, ApplyLive: true, ApplyServices: true})
	if err != nil || status != http.StatusOK {
		t.Fatalf("status=%d err=%v", status, err)
	}
	if resp.Applied || resp.ServicesApplied {
		t.Fatalf("Applied must stay false when planned service work did not run: %+v", resp)
	}
}

// #542 complement: artifacts that no runtime unit reloads (plan has no
// *_service operation) converge on promotion alone.
func TestWorkflowAppliedForPromotionOnlyRevision(t *testing.T) {
	state := &fakeState{
		plan: model.ApplyPlanResponse{
			Valid: true,
			Operations: []model.ApplyOperation{
				{Type: "promote_file", Source: "/g/sub.json", Destination: "/live/sub.json"},
			},
		},
		liveFiles: []string{"/live/sub.json"},
	}
	resp, status, err := NewWorkflow(state, nil).RunLocked(model.ApplyRequest{Confirm: true, ApplyLive: true, ApplyServices: true})
	if err != nil || status != http.StatusOK {
		t.Fatalf("status=%d err=%v", status, err)
	}
	if !resp.Applied {
		t.Fatalf("promotion-only revision should report applied: %+v", resp)
	}
}

// #541: an activation action (systemctl restart) that produced zero health
// checks is not convergence evidence — the apply must roll back and the
// rollback must stay ambiguous rather than reporting a vacuous health pass.
func TestWorkflowEmptyHealthEvidenceIsNotConvergence(t *testing.T) {
	noChecks := func([]model.ServiceActionResult) []model.ServiceHealthResult { return nil }
	state := &fakeState{
		plan:      model.ApplyPlanResponse{Valid: true},
		liveFiles: []string{"/live/x"},
		serviceActions: []model.ServiceActionResult{
			{Name: "veil-x.service", Command: []string{"systemctl", "restart", "veil-x.service"}, Success: true},
		},
		rollbackFiles: []string{"/live/x"},
		rollbackActions: []model.ServiceActionResult{
			{Name: "veil-x.service", Command: []string{"systemctl", "restart", "veil-x.service"}, Success: true},
		},
	}
	resp, status, _ := NewWorkflow(state, noChecks).RunLocked(model.ApplyRequest{Confirm: true, ApplyLive: true, ApplyServices: true})
	if status != http.StatusBadRequest {
		t.Fatalf("empty health evidence after activation must fail: status=%d", status)
	}
	if resp.Applied {
		t.Fatal("Applied must stay false without health evidence")
	}
	if resp.PostRollbackHealthPass || resp.RollbackComplete || !resp.Ambiguous {
		t.Fatalf("rollback with unproven restored health was reported complete: %+v", resp)
	}
}

// #541 complement: a pure teardown rollback (stop/disable only, nothing
// re-activated) legitimately has no health evidence — it must still be able
// to complete; the flag itself stays false because no check ran.
func TestWorkflowTeardownRollbackCompletesWithoutHealthEvidence(t *testing.T) {
	noChecks := func([]model.ServiceActionResult) []model.ServiceHealthResult { return nil }
	state := &fakeState{
		plan:      model.ApplyPlanResponse{Valid: true},
		liveFiles: []string{"/live/x"},
		serviceActions: []model.ServiceActionResult{
			{Name: "veil-x.service", Command: []string{"systemctl", "restart", "veil-x.service"}, Success: false, Error: "start failed"},
		},
		rollbackFiles: []string{"/live/x"},
		rollbackActions: []model.ServiceActionResult{
			{Name: "veil-x.service", Command: []string{"systemctl", "stop", "veil-x.service"}, Success: true},
		},
	}
	resp, status, _ := NewWorkflow(state, noChecks).RunLocked(model.ApplyRequest{Confirm: true, ApplyLive: true, ApplyServices: true})
	if status != http.StatusBadRequest {
		t.Fatalf("service failure must 400, got %d", status)
	}
	if resp.PostRollbackHealthPass {
		t.Fatal("PostRollbackHealthPass must stay false when no check ran")
	}
	if !resp.RollbackComplete || !resp.RolledBack || resp.Ambiguous {
		t.Fatalf("teardown rollback should complete: %+v", resp)
	}
}
