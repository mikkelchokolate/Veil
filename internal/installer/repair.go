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
	return NewManagedFileRepair(profile, paths).Plan()
}

func ApplyRepairPlan(plan RepairPlan) (RepairResult, error) {
	return managedfiles.Apply(plan)
}

type managedFile = managedfiles.File

func desiredManagedFiles(profile RURecommendedProfile, paths ApplyPaths) ([]managedFile, error) {
	return NewManagedFileRepair(profile, paths).desiredFiles()
}
