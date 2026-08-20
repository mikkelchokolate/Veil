package api

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	veilapply "github.com/mikkelchokolate/Veil/internal/apply"
	"github.com/mikkelchokolate/Veil/internal/caddyadmin"
	"github.com/mikkelchokolate/Veil/internal/firewall"
	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/privileged"
	"github.com/mikkelchokolate/Veil/internal/renderer"
	"github.com/mikkelchokolate/Veil/internal/routing"
	"github.com/mikkelchokolate/Veil/internal/service"
)

// caddyAdminLoader loads a Caddy JSON config through the Admin API. Tests
// override this variable to assert the Admin API path or inject failures.
var caddyAdminLoader = func(configJSON []byte) error {
	return caddyadmin.NewClient("").LoadConfig(configJSON)
}

// firewallApplier is the UFW rule applier used by apply. Tests can override it
// to avoid running real ufw commands.
type firewallApplier interface {
	EnsureActive() error
	ApplyRules(rules []firewall.Rule) error
}

var firewallApplierInstance firewallApplier = firewall.NewUFWApplier()

type ManagementApplyContext struct {
	state *managementState
	ctx   context.Context
}

func (ctx ManagementApplyContext) fenceToken() privileged.FenceToken {
	fence, ok := veilapply.FenceFromContext(ctx.operationContext())
	if !ok {
		return privileged.FenceToken{}
	}
	return privileged.FenceToken{Owner: fence.Owner, Generation: fence.Generation,
		LeaseExpiresAt: fence.LeaseExpiresAt, OperationID: fence.OperationID}
}

func NewManagementApplyContext(state *managementState) ManagementApplyContext {
	return ManagementApplyContext{state: state, ctx: state.lifecycleContext()}
}

func NewManagementApplyContextWithContext(state *managementState, operationContext context.Context) ManagementApplyContext {
	if operationContext == nil {
		operationContext = state.lifecycleContext()
	}
	return ManagementApplyContext{state: state, ctx: operationContext}
}

func (ctx ManagementApplyContext) operationContext() context.Context {
	if ctx.ctx != nil {
		return ctx.ctx
	}
	return context.TODO()
}

func (ctx ManagementApplyContext) advancePublicationPhaseLocked(phase string) error {
	return veilapply.AdvanceRuntimePublication(ctx.operationContext(), phase, veilapply.PublicationDetails{})
}

func (ctx ManagementApplyContext) buildApplyPlanLocked() ApplyPlanResponse {
	s := ctx.state
	plan := NewManagementApplyIntent(ManagementApplyIntentInput{
		ApplyRoot:     s.applyRoot,
		Settings:      s.settings,
		Inbounds:      s.inbounds,
		Rules:         s.rules,
		RoutingSource: routing.EnsureDatSource(s.routingSource, s.rules),
		Warp:          s.warp,
	}).BuildPlan()
	if validation, ok := s.enforceValidationLocked(ctx.operationContext(), s.settings, s.inbounds, s.warp); !ok {
		plan.Valid = false
		plan.Issues = append(plan.Issues, validation.Issues...)
		for _, issue := range validation.Issues {
			if issue.Severity == "error" {
				plan.Errors = managementstate.AppendUnique(plan.Errors, issue.Message)
			}
		}
	}
	return plan
}

func (ctx ManagementApplyContext) writeApplyStageLocked(plan ApplyPlanResponse) ([]string, []ConfigValidationResult, []string, error) {
	s := ctx.state
	rendered, err := s.renderManagementConfigsLocked()
	if err != nil {
		return nil, nil, nil, err
	}
	snapshot, err := s.snapshotLocked()
	if err != nil {
		return nil, nil, nil, err
	}
	return WriteApplyStage(ApplyStageInput{
		Context:       ctx.operationContext(),
		ApplyRoot:     s.applyRoot,
		Cipher:        s.cipher,
		Plan:          plan,
		Snapshot:      snapshot,
		Rendered:      rendered,
		RoutingSource: routing.EnsureDatSource(s.routingSource, s.rules),
		Validate:      stagedConfigValidator,
	})
}

func (ctx ManagementApplyContext) promoteStagedConfigs(stagedPaths []string) ([]string, []string, []livePromotionRecord, error) {
	generatedRoot := filepath.Join(ctx.state.applyRoot, "generated")
	artifactIDs := make([]string, 0, len(stagedPaths))
	for _, stagedPath := range stagedPaths {
		if _, ok := NewLiveConfigPromotion(ctx.state.applyRoot, nil).LivePathForStagedConfig(stagedPath); !ok {
			continue
		}
		relative, err := filepath.Rel(generatedRoot, stagedPath)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil, nil, nil, fmt.Errorf("staged config escapes generated root: %s", stagedPath)
		}
		artifactIDs = append(artifactIDs, filepath.ToSlash(relative))
	}
	activeFiles := make([]string, 0, len(artifactIDs))
	for _, id := range artifactIDs {
		activeFiles = append(activeFiles, filepath.Join(ctx.state.liveRoot, filepath.FromSlash(id)))
	}
	orphans, err := scanLiveConfigOrphans(ctx.state.liveRoot, activeFiles)
	if err != nil {
		return nil, nil, nil, err
	}
	var removeIDs []string
	for _, orphan := range orphans {
		relative, relErr := filepath.Rel(ctx.state.liveRoot, orphan)
		if relErr != nil {
			return nil, nil, nil, relErr
		}
		removeIDs = append(removeIDs, filepath.ToSlash(relative))
	}
	// When WARP is disabled its config is never staged, and the live sing-box
	// directory is root-owned so the panel's orphan scan can't see warp.json.
	// Drive teardown from desired state instead: if the unit is still running,
	// remove the artifact, which both deletes the live config and (via
	// UnitForArtifactID) stops and disables veil-warp.service so the egress
	// actually turns off. Gating on the running unit keeps the teardown to
	// exactly once and leaves applies that never used WARP untouched.
	if !ctx.state.warp.Enabled && ctx.warpUnitActiveLocked() &&
		!slices.Contains(removeIDs, generatedconfig.WarpConfigSubpath) {
		removeIDs = append(removeIDs, generatedconfig.WarpConfigSubpath)
	}
	// Same teardown contract for Caddy: when no naive inbound and no
	// panel-via-caddy remain, the live caddy/config.json must go or the
	// orphan scan will never touch it (config.json is excluded as a shared
	// singleton artifact) and veil-caddy.service would keep serving the
	// STALE auth_credentials forever (audit #123). Removing the artifact
	// stops and disables the unit via UnitForArtifactID.
	if !caddyRequired(ctx.state.settings, ctx.state.inbounds) && ctx.caddyUnitActiveLocked() &&
		!slices.Contains(removeIDs, generatedconfig.CaddyJSONConfigSubpath) {
		removeIDs = append(removeIDs, generatedconfig.CaddyJSONConfigSubpath)
	}
	desiredUnits := desiredRuntimeUnits(ctx.state.settings, ctx.state.inbounds, ctx.state.warp)
	wantsOrphans := scanEnabledOrphanTemplateUnits(ctx.state.systemdWantsDir, desiredUnits)
	if len(artifactIDs) == 0 && len(removeIDs) == 0 {
		// No files to promote, but leftover enabled template units still need
		// stop/disable on the subsequent service reload.
		ctx.state.orphanedUnits = wantsOrphans
		return nil, nil, nil, nil
	}
	if ctx.state.privileged == nil {
		return nil, nil, nil, fmt.Errorf("privileged helper is unavailable")
	}
	publicationArtifacts := append(append([]string(nil), artifactIDs...), removeIDs...)
	expectedManifest, previousManifest, err := publicationArtifactDigests(generatedRoot, ctx.state.liveRoot, artifactIDs, removeIDs)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("build publication manifest: %w", err)
	}
	if err := veilapply.MarkRuntimeMutationStarting(ctx.operationContext(), veilapply.PublicationDetails{
		ExpectedLiveManifestSHA256: expectedManifest,
		PreviousLiveManifestSHA256: previousManifest,
		Artifacts:                  publicationArtifacts,
		ServicePhase:               "pending",
		FirewallPhase:              "pending",
		LiveRoot:                   ctx.state.liveRoot,
	}); err != nil {
		return nil, nil, nil, fmt.Errorf("persist publication phase before promotion: %w", err)
	}
	result, err := ctx.state.privileged.Promote(ctx.operationContext(), privileged.PromoteRequest{
		ArtifactIDs: artifactIDs, RemoveArtifactIDs: removeIDs, Fence: ctx.fenceToken(),
	})
	if err != nil {
		return nil, nil, nil, err
	}
	if err := veilapply.AdvanceRuntimePublication(ctx.operationContext(), veilapply.PublicationPhaseArtifactsCommitted, veilapply.PublicationDetails{}); err != nil {
		return nil, nil, nil, fmt.Errorf("persist committed artifact publication phase: %w", err)
	}
	liveFiles := livePathsForArtifactIDs(ctx.state.liveRoot, result.WrittenArtifacts)
	records := make([]livePromotionRecord, 0, len(result.WrittenArtifacts)+len(result.RemovedArtifacts))
	for _, id := range append(append([]string{}, result.WrittenArtifacts...), result.RemovedArtifacts...) {
		records = append(records, livePromotionRecord{
			ArtifactID: id,
			BackupID:   result.BackupID,
			LivePath:   filepath.Join(ctx.state.liveRoot, filepath.FromSlash(id)),
		})
	}
	backupFiles := append([]string(nil), result.BackupArtifacts...)
	var orphanedUnits []string
	for _, id := range result.RemovedArtifacts {
		if unit, ok := UnitForArtifactID(id); ok {
			orphanedUnits = append(orphanedUnits, unit)
		}
	}
	ctx.state.orphanedUnits = mergeOrphanedUnits(orphanedUnits, wantsOrphans)
	return liveFiles, backupFiles, records, nil
}

func (ctx ManagementApplyContext) reloadPromotedServices(liveFiles []string) []ServiceActionResult {
	results := []ServiceActionResult{}

	// Retire legacy per-inbound Caddy instances before touching the singleton.
	// All of those processes use SO_REUSEPORT and the same Admin API address, so
	// an Admin API load performed first can be accepted by the wrong process and
	// falsely report success while veil-caddy.service remains inactive.
	remainingOrphans := make([]string, 0, len(ctx.state.orphanedUnits))
	for _, unit := range ctx.state.orphanedUnits {
		if !isTemplateCaddyUnit(unit) {
			remainingOrphans = append(remainingOrphans, unit)
			continue
		}
		stop := ctx.runPrivilegedServiceAction(unit, privileged.ServiceActionStop)
		results = append(results, stop)
		if !stop.Success {
			return results
		}
		disable := ctx.runPrivilegedServiceAction(unit, privileged.ServiceActionDisable)
		results = append(results, disable)
		if !disable.Success {
			return results
		}
	}
	ctx.state.orphanedUnits = remainingOrphans

	// Phase 1: Load Caddy config first. This must happen before hysteria2
	// certificate synchronization so that Caddy can begin (or complete) ACME
	// issuance for newly referenced hysteria2 domains.
	for _, runtime := range NewManagedRuntimeCatalogFor(ctx.state.settings, ctx.state.inbounds, ctx.state.warp).Runtimes() {
		if runtime.Unit != unitCaddy {
			continue
		}
		if runtime.PromotedSubpath == "" || runtime.PromotedVerb == "" {
			continue
		}
		want := filepath.Join(ctx.state.liveRoot, filepath.FromSlash(runtime.PromotedSubpath))
		if !containsCleanPath(liveFiles, want) {
			continue
		}
		// For the consolidated Caddy unit, prefer loading the runtime config
		// through Caddy's Admin API and only fall back to systemctl reload when
		// the Admin API is unavailable.
		caddyLivePath := filepath.Join(ctx.state.liveRoot, "caddy", "config.json")
		var adminLoadErr error
		if containsCleanPath(liveFiles, caddyLivePath) {
			// Use a synthetic command for the Admin API load. There is no systemctl
			// invocation here; the REST response contract still expects a Command
			// array, so we report the Caddy admin endpoint that was used.
			adminResult := ServiceActionResult{
				Name:    unitCaddy,
				Command: []string{"caddy", "admin", "load"},
			}
			configBytes, err := os.ReadFile(caddyLivePath)
			if err == nil {
				if loader, ok := ctx.state.privileged.(privileged.CaddyLoader); ok {
					err = loader.CaddyLoad(ctx.operationContext(), privileged.CaddyLoadRequest{Config: configBytes, Fence: ctx.fenceToken()})
				} else if ctx.state.privilegedLocal {
					err = caddyAdminLoader(configBytes)
				} else {
					err = errors.New("privileged Caddy loader is unavailable")
				}
			}
			if err == nil {
				adminResult.Success = true
				results = append(results, adminResult)
				continue
			}
			// An inactive singleton has no Admin API yet during the first migration
			// from legacy template units. Defer reporting this miss until the
			// systemd reload/start fallback has also failed.
			adminLoadErr = err
		}
		result := ctx.runPrivilegedServiceAction(runtime.Unit, privileged.ServiceAction(runtime.PromotedVerb))
		if !result.Success && adminLoadErr != nil {
			if result.Error == "" {
				result.Error = fmt.Sprintf("caddy admin load failed: %v; systemd fallback failed", adminLoadErr)
			} else {
				result.Error = fmt.Sprintf("caddy admin load failed: %v; systemd fallback failed: %s", adminLoadErr, result.Error)
			}
		}
		results = append(results, result)
		if !result.Success {
			return results
		}
	}

	// Phase 2: Synchronize hysteria2 certificates after Caddy has reloaded so
	// new domains have a chance to obtain a certificate before hysteria2 starts.
	if ctx.hysteria2ConfigReloadNeeded(liveFiles) {
		for _, domain := range ctx.hysteria2DomainsLocked() {
			results = append(results, ctx.syncCaddyCertForHysteria2(domain))
			if !results[len(results)-1].Success {
				return results
			}
		}
	}

	// Phase 3: Reload remaining services (including hysteria2).
	for _, runtime := range NewManagedRuntimeCatalogFor(ctx.state.settings, ctx.state.inbounds, ctx.state.warp).Runtimes() {
		if runtime.Unit == unitCaddy {
			continue
		}
		if runtime.PromotedSubpath == "" || runtime.PromotedVerb == "" {
			continue
		}
		want := filepath.Join(ctx.state.liveRoot, filepath.FromSlash(runtime.PromotedSubpath))
		if !containsCleanPath(liveFiles, want) {
			continue
		}
		// Re-enable WARP for boot persistence: teardown disables the unit, so a
		// later restart alone would leave it disabled and gone after a reboot.
		if runtime.Unit == renderer.UnitWarp && ctx.state.warp.Enabled {
			enable := ctx.runPrivilegedServiceAction(runtime.Unit, privileged.ServiceActionEnable)
			results = append(results, enable)
			if !enable.Success {
				return results
			}
		}
		result := ctx.runPrivilegedServiceAction(runtime.Unit, privileged.ServiceAction(runtime.PromotedVerb))
		results = append(results, result)
		if !result.Success {
			return results
		}
		// Enable protocol units for boot persistence. The installer only
		// enables veil.service, veil-helper.socket and (optionally)
		// veil-caddy.service; per-instance protocol units (hysteria2/olcrtc)
		// and the mieru singleton are otherwise dead after a reboot with no
		// Veil-side signal (audit #117/#137). WARP is handled above; caddy and
		// the panel unit are handled by the installer.
		if runtime.Unit != renderer.UnitVeil && runtime.Unit != unitCaddy && runtime.Unit != renderer.UnitWarp {
			enable := ctx.runPrivilegedServiceAction(runtime.Unit, privileged.ServiceActionEnable)
			results = append(results, enable)
			// A failed enable must not abort the rest of the reload: the
			// remaining restarts and the orphan stop/disable phase still
			// need to run (code-review P3). The failed result is already
			// recorded for the caller.
		}
	}

	// Stop and disable units whose configs were removed after the new
	// configuration has taken their ports. This ordering prevents a brief
	// window where an old service would rebind a port needed by the new config.
	if len(ctx.state.orphanedUnits) > 0 {
		for _, unit := range ctx.state.orphanedUnits {
			stop := ctx.runPrivilegedServiceAction(unit, privileged.ServiceActionStop)
			results = append(results, stop)
			if !stop.Success {
				return results
			}
			disable := ctx.runPrivilegedServiceAction(unit, privileged.ServiceActionDisable)
			results = append(results, disable)
			if !disable.Success {
				return results
			}
		}
	}
	ctx.state.orphanedUnits = nil

	return results
}

func (ctx ManagementApplyContext) rollbackPromotedConfigs(records []livePromotionRecord, liveFiles []string) ([]string, []ServiceActionResult) {
	if len(records) == 0 || records[0].BackupID == "" || ctx.state.privileged == nil {
		return nil, nil
	}
	result, err := ctx.state.privileged.Promote(ctx.operationContext(), privileged.PromoteRequest{
		RestoreBackupID: records[0].BackupID, Fence: ctx.fenceToken(),
	})
	if err != nil {
		return nil, []ServiceActionResult{{
			Name: "promotion-rollback", Command: []string{"helper", "promote", "restore"},
			Success: false, Error: err.Error(),
		}}
	}
	rollbackFiles := livePathsForArtifactIDs(ctx.state.liveRoot, result.WrittenArtifacts)
	// The units removed during the failed apply are about to be restored; do not
	// stop/disable them again while reloading services for the restored state.
	ctx.state.orphanedUnits = nil
	rollbackActions := ctx.reloadPromotedServices(rollbackFiles)

	liveFilesMap := make(map[string]bool)
	for _, lf := range liveFiles {
		liveFilesMap[filepath.Clean(lf)] = true
	}
	rollbackFilesMap := make(map[string]bool)
	for _, rf := range rollbackFiles {
		rollbackFilesMap[filepath.Clean(rf)] = true
	}
	var restoredUnits []string
	var removedNewUnits []string
	for _, record := range records {
		cleanPath := filepath.Clean(record.LivePath)
		if liveFilesMap[cleanPath] && !rollbackFilesMap[cleanPath] {
			// The file was promoted (so its unit may have been started) but the
			// restore did not bring it back. This happens for a config that was
			// newly added during the failed apply and had no previous version:
			// restore deletes it. Stop+disable its unit so rollback does not
			// leave a service running against a now-deleted config.
			if unit, ok := UnitForArtifactID(record.ArtifactID); ok {
				removedNewUnits = append(removedNewUnits, unit)
			}
			continue
		}
		if liveFilesMap[cleanPath] {
			// File stayed live after the apply; it was only updated, so the
			// service reload above already applied the restored config.
			continue
		}
		if !rollbackFilesMap[cleanPath] {
			// The file was removed during apply but was not restored; leave the
			// unit stopped.
			continue
		}
		if unit, ok := UnitForLiveConfig(record.LivePath); ok {
			restoredUnits = append(restoredUnits, unit)
		}
	}
	// Stop newly-added units whose configs were removed by the restore before
	// bringing restored units back up.
	for _, unit := range removedNewUnits {
		stop := ctx.runPrivilegedServiceAction(unit, privileged.ServiceActionStop)
		rollbackActions = append(rollbackActions, stop)
		if !stop.Success {
			return rollbackFiles, rollbackActions
		}
		disable := ctx.runPrivilegedServiceAction(unit, privileged.ServiceActionDisable)
		rollbackActions = append(rollbackActions, disable)
		if !disable.Success {
			return rollbackFiles, rollbackActions
		}
	}
	if len(restoredUnits) > 0 {
		restoresLegacyCaddy := false
		for _, unit := range restoredUnits {
			if isTemplateCaddyUnit(unit) {
				restoresLegacyCaddy = true
				break
			}
		}
		if restoresLegacyCaddy {
			// A failed consolidated-Caddy migration must not restart the restored
			// template instances while the singleton is still running; both use
			// SO_REUSEPORT on the public listener and the shared Admin API.
			stop := ctx.runPrivilegedServiceAction(renderer.UnitCaddy, privileged.ServiceActionStop)
			rollbackActions = append(rollbackActions, stop)
			if !stop.Success {
				return rollbackFiles, rollbackActions
			}
			disable := ctx.runPrivilegedServiceAction(renderer.UnitCaddy, privileged.ServiceActionDisable)
			rollbackActions = append(rollbackActions, disable)
			if !disable.Success {
				return rollbackFiles, rollbackActions
			}
		}
		for _, unit := range restoredUnits {
			rollbackActions = append(rollbackActions,
				ctx.runPrivilegedServiceAction(unit, privileged.ServiceActionEnable),
				ctx.runPrivilegedServiceAction(unit, privileged.ServiceActionStart),
			)
		}
	}
	return rollbackFiles, rollbackActions
}

func (ctx ManagementApplyContext) hysteria2ConfigReloadNeeded(liveFiles []string) bool {
	for _, runtime := range NewManagedRuntimeCatalogFor(ctx.state.settings, ctx.state.inbounds, ctx.state.warp).Runtimes() {
		if runtime.Protocol != "hysteria2" {
			continue
		}
		if runtime.PromotedSubpath == "" {
			continue
		}
		want := filepath.Join(ctx.state.liveRoot, filepath.FromSlash(runtime.PromotedSubpath))
		if containsCleanPath(liveFiles, want) {
			return true
		}
	}
	return false
}

func (ctx ManagementApplyContext) hysteria2DomainsLocked() []string {
	seen := make(map[string]struct{})
	var domains []string
	for _, inb := range ctx.state.inbounds {
		if !inb.Enabled || inb.Protocol != "hysteria2" {
			continue
		}
		// Only sync a Caddy cert when the hysteria2 inbound actually serves a
		// Caddy-managed certificate. This must mirror the renderer's cert
		// selection (inbound_renderer.go): the Caddy cert is used only when
		// PanelAccess == "caddy" or the inbound has its own per-inbound domain.
		// Otherwise the inbound serves the panel cert and requires no sync;
		// syncing would block apply on cert polling and abort before the
		// service restart (regression: auto-apply produced no service action).
		if ctx.state.settings.PanelAccess != "caddy" && model.InboundDomain(inb) == "" {
			continue
		}
		domain := model.ResolveInboundDomain(inb, ctx.state.settings)
		if domain == "" {
			continue
		}
		if _, ok := seen[domain]; ok {
			continue
		}
		seen[domain] = struct{}{}
		domains = append(domains, domain)
	}
	return domains
}

func (ctx ManagementApplyContext) syncCaddyCertForHysteria2(domain string) ServiceActionResult {
	result := ServiceActionResult{
		Name:    "sync-caddy-cert",
		Command: []string{"helper", "sync_caddy_cert", domain},
	}
	if ctx.state.privileged == nil {
		result.Error = "privileged helper is unavailable"
		return result
	}
	syncResult, err := ctx.state.privileged.SyncCaddyCert(ctx.operationContext(), privileged.SyncCaddyCertRequest{
		Domain: domain,
		OutDir: "/etc/veil/certs",
		Fence:  ctx.fenceToken(),
	})
	if err != nil {
		result.Error = err.Error()
		return result
	}
	if !syncResult.Found {
		result.Error = "Caddy has not yet issued a certificate for " + domain + "; ensure the domain resolves to this server and Cloudflare proxy is disabled so ACME can complete"
		return result
	}
	result.Success = true
	return result
}

func (ctx ManagementApplyContext) PrepareFirewallLocked() (string, error) {
	if ctx.state.settings.FirewallManagement != nil && !*ctx.state.settings.FirewallManagement {
		return "", nil
	}
	if ctx.state.privileged == nil || ctx.state.privilegedLocal {
		results := ctx.syncFirewall()
		for _, result := range results {
			if !result.Success {
				return "", errors.New(result.Error)
			}
		}
		return "", nil
	}
	responses := firewall.BuildRuleResponses(ctx.state.settings, ctx.state.inbounds)
	rules := firewall.UFWRulesFromResponses(responses)
	if len(rules) == 0 {
		return "", nil
	}
	reqRules := make([]privileged.FirewallRule, len(rules))
	for index, rule := range rules {
		reqRules[index] = privileged.FirewallRule{Command: rule.Command, Args: rule.Args}
	}
	result, err := ctx.state.privileged.FirewallApply(ctx.operationContext(), privileged.FirewallRequest{
		Rules: reqRules, Action: privileged.FirewallActionPrepare, Fence: ctx.fenceToken(),
	})
	if err != nil {
		return "", err
	}
	if !result.Prepared || result.TransactionID == "" {
		return "", errors.New("privileged helper did not prepare firewall transaction")
	}
	return result.TransactionID, nil
}

func (ctx ManagementApplyContext) CommitFirewallLocked(transactionID string) error {
	if transactionID == "" || ctx.state.privileged == nil || ctx.state.privilegedLocal {
		return nil
	}
	_, err := ctx.state.privileged.FirewallApply(ctx.operationContext(), privileged.FirewallRequest{
		Action: privileged.FirewallActionCommit, TransactionID: transactionID, Fence: ctx.fenceToken(),
	})
	return err
}

func (ctx ManagementApplyContext) RollbackFirewallLocked(transactionID string) error {
	if transactionID == "" || ctx.state.privileged == nil || ctx.state.privilegedLocal {
		return nil
	}
	_, err := ctx.state.privileged.FirewallApply(ctx.operationContext(), privileged.FirewallRequest{
		Action: privileged.FirewallActionRollback, TransactionID: transactionID, Fence: ctx.fenceToken(),
	})
	return err
}

func (ctx ManagementApplyContext) syncFirewall() []ServiceActionResult {
	// Firewall management is enabled by default. A nil pointer means the setting
	// is absent from an older state file, which we treat as enabled.
	if ctx.state.settings.FirewallManagement != nil && !*ctx.state.settings.FirewallManagement {
		return nil
	}
	responses := firewall.BuildRuleResponses(ctx.state.settings, ctx.state.inbounds)
	rules := firewall.UFWRulesFromResponses(responses)
	if len(rules) == 0 {
		return nil
	}
	result := ServiceActionResult{
		Name:    "sync-firewall",
		Command: []string{"ufw", "sync-rules"},
	}
	// Use the privileged helper when available (production). It runs as root and
	// can execute ufw. Fall back to the local applier in dev/test mode.
	if ctx.state.privileged != nil && !ctx.state.privilegedLocal {
		reqRules := make([]privileged.FirewallRule, len(rules))
		for i, r := range rules {
			reqRules[i] = privileged.FirewallRule{Command: r.Command, Args: r.Args}
		}
		if _, err := ctx.state.privileged.FirewallApply(ctx.operationContext(), privileged.FirewallRequest{Rules: reqRules, Fence: ctx.fenceToken()}); err != nil {
			result.Error = err.Error()
			return []ServiceActionResult{result}
		}
	} else {
		if err := firewallApplierInstance.EnsureActive(); err != nil {
			result.Error = err.Error()
			return []ServiceActionResult{result}
		}
		if err := firewallApplierInstance.ApplyRules(rules); err != nil {
			result.Error = err.Error()
			return []ServiceActionResult{result}
		}
	}
	result.Success = true
	return []ServiceActionResult{result}
}

func (ctx ManagementApplyContext) runPrivilegedServiceAction(unit string, action privileged.ServiceAction) ServiceActionResult {
	result := ServiceActionResult{
		Name: unit, Command: []string{"systemctl", string(action), unit},
	}
	if ctx.state.privileged == nil {
		result.Error = "privileged helper is unavailable"
		return result
	}
	if err := ctx.state.privileged.ServiceAction(ctx.operationContext(), privileged.ServiceActionRequest{
		Unit: unit, Action: action, Fence: ctx.fenceToken(),
	}); err != nil {
		result.Error = err.Error()
		return result
	}
	result.Success = true
	return result
}

// warpUnitActiveLocked reports whether veil-warp.service is currently running,
// so a WARP teardown only happens when there is actually something to stop.
func (ctx ManagementApplyContext) warpUnitActiveLocked() bool {
	if ctx.state.privileged == nil {
		return false
	}
	statuses, err := ctx.state.privileged.ServiceStatus(ctx.operationContext(), privileged.ServiceStatusRequest{
		Units: []string{renderer.UnitWarp},
	})
	if err != nil {
		return false
	}
	for _, status := range statuses.Services {
		if status.Unit == renderer.UnitWarp && status.ActiveState == "active" {
			return true
		}
	}
	return false
}

// caddyUnitActiveLocked reports whether veil-caddy.service is currently
// active, used to gate Caddy config teardown on desired state.
func (ctx ManagementApplyContext) caddyUnitActiveLocked() bool {
	if ctx.state.privileged == nil {
		return false
	}
	statuses, err := ctx.state.privileged.ServiceStatus(ctx.operationContext(), privileged.ServiceStatusRequest{
		Units: []string{unitCaddy},
	})
	if err != nil {
		return false
	}
	for _, status := range statuses.Services {
		if status.Unit == unitCaddy && status.ActiveState == "active" {
			return true
		}
	}
	return false
}

func livePathsForArtifactIDs(liveRoot string, ids []string) []string {
	paths := make([]string, 0, len(ids))
	for _, id := range ids {
		paths = append(paths, filepath.Join(liveRoot, filepath.FromSlash(id)))
	}
	sort.Strings(paths)
	return paths
}

func containsCleanPath(paths []string, want string) bool {
	want = filepath.Clean(want)
	for _, path := range paths {
		if filepath.Clean(path) == want {
			return true
		}
	}
	return false
}

func (ctx ManagementApplyContext) appendApplyHistoryLocked(stage string, success bool, response ApplyResponse) error {
	return ctx.state.applyHistoryLocked().Append(stage, success, response)
}

func filterHealthCheckableActions(actions []ServiceActionResult) []ServiceActionResult {
	catalog := NewManagedRuntimeCatalog()
	seen := make(map[string]struct{})
	out := make([]ServiceActionResult, 0, len(actions))
	for _, action := range actions {
		if !action.Success || action.Name == "" || !catalog.AllowsHealthUnit(action.Name) || !serviceActionRequiresActiveUnit(action) {
			continue
		}
		if _, ok := seen[action.Name]; ok {
			continue
		}
		seen[action.Name] = struct{}{}
		out = append(out, action)
	}
	return out
}

func serviceActionRequiresActiveUnit(action ServiceActionResult) bool {
	if len(action.Command) >= 2 && action.Command[0] == "systemctl" {
		switch privileged.ServiceAction(action.Command[1]) {
		case privileged.ServiceActionStart, privileged.ServiceActionRestart, privileged.ServiceActionReload:
			return true
		}
	}
	return len(action.Command) >= 3 && action.Command[0] == "caddy" && action.Command[1] == "admin" && action.Command[2] == "load"
}

func (ctx ManagementApplyContext) checkServiceHealth(actions []ServiceActionResult) []ServiceHealthResult {
	healthActions := filterHealthCheckableActions(actions)
	if ctx.state.privilegedLocal {
		return service.NewServiceHealthCollection(func(name string) ServiceHealthResult {
			return serviceHealthChecker(name)
		}).Check(healthActions)
	}
	units := make([]string, 0, len(healthActions))
	for _, action := range healthActions {
		units = append(units, action.Name)
	}
	if len(units) == 0 {
		return nil
	}
	statuses, err := ctx.state.privileged.ServiceStatus(ctx.operationContext(), privileged.ServiceStatusRequest{Units: units})
	if err != nil {
		return []ServiceHealthResult{{
			Name: "managed-services", Command: []string{"helper", "service-status"},
			Healthy: false, Error: err.Error(),
		}}
	}
	byUnit := make(map[string]privileged.ServiceStatus, len(statuses.Services))
	for _, status := range statuses.Services {
		byUnit[status.Unit] = status
	}
	results := make([]ServiceHealthResult, 0, len(units))
	for _, unit := range units {
		status := byUnit[unit]
		healthy := status.ActiveState == "active" && status.Error == ""
		results = append(results, ServiceHealthResult{
			Name: unit, Command: []string{"helper", "service-status", unit},
			Healthy: healthy, Output: status.ActiveState + "/" + status.SubState, Error: status.Error,
		})
	}
	return results
}
