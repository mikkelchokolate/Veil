package installer

import (
	"errors"
	"os"
	"syscall"
)

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
	plan := RepairPlan{}
	for _, file := range files {
		body, err := os.ReadFile(file.Path)
		if err != nil {
			if isMissingOrBlocked(err) {
				plan.Actions = append(plan.Actions, RepairAction{Path: file.Path, Reason: RepairReasonMissing, Content: file.Content, Mode: file.Mode})
				continue
			}
			return RepairPlan{}, err
		}
		if string(body) != file.Content {
			plan.Actions = append(plan.Actions, RepairAction{Path: file.Path, Reason: RepairReasonDrifted, Content: file.Content, Mode: file.Mode})
		}
	}
	return plan, nil
}

func (r ManagedFileRepair) Apply(plan RepairPlan) (RepairResult, error) {
	result := RepairResult{}
	for _, action := range plan.Actions {
		if err := writeManagedFile(action.Path, action.Content, action.Mode); err != nil {
			return RepairResult{}, err
		}
		result.WrittenFiles = append(result.WrittenFiles, action.Path)
	}
	return result, nil
}

func (r ManagedFileRepair) desiredFiles() ([]managedFile, error) {
	return NewPanelManagedMaterialFromProfile(r.profile, r.paths).files()
}

// isMissingOrBlocked reports whether err means the file cannot be read
// because it doesn't exist or a path component is not a directory (ENOTDIR).
func isMissingOrBlocked(err error) bool {
	return os.IsNotExist(err) || errors.Is(err, syscall.ENOTDIR)
}
