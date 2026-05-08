package installer

import (
	"fmt"
	"os"
	"strings"
)

type RepairReason string

const (
	RepairReasonMissing RepairReason = "missing"
	RepairReasonDrifted RepairReason = "drifted"
)

type RepairAction struct {
	Path    string
	Reason  RepairReason
	Content string
	Mode    os.FileMode
}

type RepairPlan struct {
	Actions []RepairAction
}

type RepairResult struct {
	WrittenFiles []string
}

func BuildRepairPlan(profile RURecommendedProfile, paths ApplyPaths) (RepairPlan, error) {
	return NewManagedFileRepair(profile, paths).Plan()
}

func (p RepairPlan) HasChanges() bool {
	return len(p.Actions) > 0
}

func (p RepairPlan) Summary() string {
	if len(p.Actions) == 0 {
		return "No repair actions required\n"
	}
	var b strings.Builder
	for _, action := range p.Actions {
		fmt.Fprintf(&b, "repair %s %s\n", action.Reason, action.Path)
	}
	return b.String()
}

func ApplyRepairPlan(plan RepairPlan) (RepairResult, error) {
	return ManagedFileRepair{}.Apply(plan)
}

type managedFile struct {
	Path    string
	Content string
	Mode    os.FileMode
}

func desiredManagedFiles(profile RURecommendedProfile, paths ApplyPaths) ([]managedFile, error) {
	return NewManagedFileRepair(profile, paths).desiredFiles()
}
