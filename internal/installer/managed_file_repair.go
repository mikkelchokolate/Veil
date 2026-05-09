package installer

import "github.com/veil-panel/veil/internal/managedfiles"

type ManagedFileRepair struct {
	profile RURecommendedProfile
	paths   ApplyPaths
}

func NewManagedFileRepair(profile RURecommendedProfile, paths ApplyPaths) ManagedFileRepair {
	return ManagedFileRepair{profile: profile, paths: paths}
}

func (r ManagedFileRepair) Plan() (RepairPlan, error) {
	files, err := r.desiredFiles()
	if err != nil {
		return RepairPlan{}, err
	}
	return managedfiles.NewSet(files).Plan()
}

func (r ManagedFileRepair) Apply(plan RepairPlan) (RepairResult, error) {
	return managedfiles.Apply(plan)
}

func (r ManagedFileRepair) desiredFiles() ([]managedFile, error) {
	return NewPanelManagedMaterialFromProfile(r.profile, r.paths).files()
}

func isMissingOrBlocked(err error) bool {
	return managedfiles.IsMissingOrBlocked(err)
}
