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
	return managed, nil
}
