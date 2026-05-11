package installer

import (
	"github.com/veil-panel/veil/internal/managedfiles"
	"github.com/veil-panel/veil/internal/panelmaterial"
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
	return managedfiles.Apply(plan)
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
		Caddyfile:         profile.Caddyfile,
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
