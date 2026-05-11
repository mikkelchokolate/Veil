package installer

import "github.com/veil-panel/veil/internal/managedfiles"

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
	return NewPanelManagedMaterialFromProfile(profile, paths).files()
}
