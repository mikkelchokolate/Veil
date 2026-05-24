package managedfiles

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

type File struct {
	Path    string
	Content string
	Mode    os.FileMode
}

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

type Set struct {
	files []File
}

func NewSet(files []File) Set {
	out := make([]File, len(files))
	copy(out, files)
	return Set{files: out}
}

func (s Set) Plan() (RepairPlan, error) {
	plan := RepairPlan{}
	for _, file := range s.files {
		body, err := os.ReadFile(file.Path)
		if err != nil {
			if IsMissingOrBlocked(err) {
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

func (p RepairPlan) HasChanges() bool {
	return len(p.Actions) > 0
}

func (p RepairPlan) Summary() string {
	if len(p.Actions) == 0 {
		return "No repair actions required\n"
	}
	var b strings.Builder
	for _, action := range p.Actions {
		fmt.Fprintf(&b, "repair %s %s\n", action.Reason, filepath.ToSlash(action.Path))
	}
	return b.String()
}

func Apply(plan RepairPlan) (RepairResult, error) {
	result := RepairResult{}
	for _, action := range plan.Actions {
		if err := WriteFile(action.Path, action.Content, action.Mode); err != nil {
			return RepairResult{}, err
		}
		result.WrittenFiles = append(result.WrittenFiles, action.Path)
	}
	return result, nil
}

func WriteFile(path string, content string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func IsMissingOrBlocked(err error) bool {
	return os.IsNotExist(err) || errors.Is(err, syscall.ENOTDIR)
}
