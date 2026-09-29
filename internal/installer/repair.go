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

// ApplyRepairPlan applies the planned repairs and restores the ownership
// contract. etcDir is the configured Veil etc directory: runtime-shared
// classification is prefix-matched against it so a custom install layout
// cannot mark unrelated paths veil-proxy-shared (#1130).
func ApplyRepairPlan(plan RepairPlan, etcDir string) (RepairResult, error) {
	result, err := managedfiles.Apply(plan)
	if err != nil {
		return RepairResult{}, err
	}
	// Repaired files land as root:root with the plan mode; restore the install
	// ownership contract (veil.env/state keys → root:veil 0640, generated and
	// panel TLS → root:veil-proxy 0640) so the runtime units can read them
	// again (audit #379).
	if err := chownSecretsForVeilGroup(result.WrittenFiles, etcDir); err != nil {
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
		ACMEInsecure:      profile.ACMEInsecure,
		PanelPublicIP:     profile.PanelPublicIP,
		PanelLEIPCert:     profile.PanelLEIPCertEnv,
		PanelHTTP01Port:   profile.PanelHTTP01PortEnv,
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
		managed[i].Owner = desiredFileOwner(managed[i].Path, paths.EtcDir, veilGID, proxyGID)
	}
	return managed, nil
}

// desiredFileOwner maps a managed file onto the post-#601 ownership contract:
// runtime-shared material (<etcDir>/{generated,tls,panel,certs,www}) is
// root:veil-proxy, panel-only secrets are root:veil, and everything else
// (systemd units) stays root:root.
func desiredFileOwner(path, etcDir string, veilGID, proxyGID int) *managedfiles.FileOwner {
	switch {
	case isRuntimeSharedConfig(path, etcDir):
		return &managedfiles.FileOwner{UID: 0, GID: proxyGID}
	case needsVeilGroupRead(path, etcDir):
		return &managedfiles.FileOwner{UID: 0, GID: veilGID}
	default:
		return &managedfiles.FileOwner{UID: 0, GID: 0}
	}
}
