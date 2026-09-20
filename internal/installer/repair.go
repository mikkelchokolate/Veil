package installer

import (
	"github.com/mikkelchokolate/Veil/internal/managedfiles"
	"github.com/mikkelchokolate/Veil/internal/panelmaterial"
)

type RepairReason = managedfiles.RepairReason

const (
	RepairReasonMissing RepairReason = managedfiles.RepairReasonMissing
	RepairReasonDrifted RepairReason = managedfiles.RepairReasonDrifted
)

type RepairAction = managedfiles.RepairAction
type RepairPlan = managedfiles.RepairPlan
type RepairResult = managedfiles.RepairResult

func BuildRepairPlan(profile RURecommendedProfile, paths ApplyPaths) (RepairPlan, error) {
	files, err := desiredManagedFiles(profile, paths)
	if err != nil {
		return RepairPlan{}, err
	}
	return managedfiles.NewSet(files).Plan()
}

func ApplyRepairPlan(plan RepairPlan) (RepairResult, error) {
	result, err := managedfiles.Apply(plan)
	if err != nil {
		return RepairResult{}, err
	}
	// Repaired files land as root:root with the plan mode; restore the install
	// ownership contract (veil.env/state keys → root:veil 0640, generated and
	// panel TLS → root:veil-proxy 0640) so the runtime units can read them
	// again (audit #379).
	if err := chownSecretsForVeilGroup(result.WrittenFiles); err != nil {
		return result, err
	}
	return result, nil
}

type managedFile = managedfiles.File

func desiredManagedFiles(profile RURecommendedProfile, paths ApplyPaths) ([]managedFile, error) {
	files, err := panelmaterial.NewManagedMaterial(panelmaterial.Input{
		Paths:             panelmaterial.Paths{EtcDir: paths.EtcDir, VarDir: paths.VarDir, SystemdDir: paths.SystemdDir, VeilBinary: paths.VeilBinary, CaddyBinary: paths.CaddyBinary},
		PanelAuthToken:    profile.PanelAuthToken,
		PanelListen:       profile.PanelListen,
		PanelAccess:       profile.PanelAccess,
		Domain:            profile.Domain,
		Email:             profile.Email,
		WebBasePath:       profile.WebBasePath,
		PanelTLSEnabled:   profile.PanelTLSEnabled,
		PanelTLSCertPEM:   profile.PanelTLSCertPEM,
		PanelTLSKeyPEM:    profile.PanelTLSKeyPEM,
		InstallPanelCaddy: profile.InstallPanelCaddy,
		CaddyJSON:         profile.CaddyJSON,
		ACMECAURL:         profile.ACMECAURL,
		ACMECARoot:        profile.ACMECARoot,
	}).Files()
	if err != nil {
		return nil, err
	}
	managed := make([]managedFile, 0, len(files))
	for _, file := range files {
		managed = append(managed, managedFile{Path: file.Path, Content: file.Content, Mode: file.Mode})
	}
	if effectiveUID() != 0 {
		// Non-root processes cannot inspect or restore the ownership contract;
		// plan without it (Apply would fail closed on the packaged layout).
		return managed, nil
	}
	// Attach the ownership contract so Plan() flags ownership-only drift —
	// root:root or veil-grouped secrets must be repaired, not silently kept
	// (audit #518). Unresolvable runtime accounts fail the plan closed: the
	// apply step would be unable to restore ownership anyway (audit #532).
	veilGID, err := resolveGroupGID("veil")
	if err != nil {
		return nil, err
	}
	proxyGID, err := resolveGroupGID("veil-proxy")
	if err != nil {
		return nil, err
	}
	for i := range managed {
		managed[i].Owner = desiredFileOwner(managed[i].Path, veilGID, proxyGID)
	}
	return managed, nil
}

// desiredFileOwner maps a managed file onto the post-#601 ownership contract:
// runtime-shared material (generated/, tls/, panel/, certs/, www/) is
// root:veil-proxy, panel-only secrets are root:veil, and everything else
// (systemd units) stays root:root.
func desiredFileOwner(path string, veilGID, proxyGID int) *managedfiles.FileOwner {
	switch {
	case isRuntimeSharedConfig(path):
		return &managedfiles.FileOwner{UID: 0, GID: proxyGID}
	case needsVeilGroupRead(path):
		return &managedfiles.FileOwner{UID: 0, GID: veilGID}
	default:
		return &managedfiles.FileOwner{UID: 0, GID: 0}
	}
}
