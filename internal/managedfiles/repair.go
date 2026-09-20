package managedfiles

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/mikkelchokolate/Veil/internal/atomicfile"
)

// FileOwner is the required numeric ownership for a managed file. Files that
// must stay reachable by a specific runtime group (veil, veil-proxy) carry an
// Owner so Plan can flag ownership-only drift and Apply can restore it.
type FileOwner struct {
	UID int
	GID int
}

type File struct {
	Path    string
	Content string
	Mode    os.FileMode
	// DirMode is applied to parent directories WriteFile creates; zero keeps
	// the restrictive default (0o750, audit #519).
	DirMode os.FileMode
	// Owner, when non-nil, makes Plan treat ownership-only drift as a repair
	// action and makes Apply re-apply the owner after writing (audit #518).
	// Nil disables ownership checks (e.g. non-root dry-run planning).
	Owner *FileOwner
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
	DirMode os.FileMode
	Owner   *FileOwner
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
		action := RepairAction{Path: file.Path, Content: file.Content, Mode: file.Mode, DirMode: file.DirMode, Owner: file.Owner}
		info, err := os.Lstat(file.Path)
		if err != nil {
			if IsMissingOrBlocked(err) {
				action.Reason = RepairReasonMissing
				plan.Actions = append(plan.Actions, action)
				continue
			}
			return RepairPlan{}, err
		}
		// A directory at a managed path cannot be repaired by an atomic file
		// write — it is corruption outside the repair contract, so fail the
		// plan rather than emit an action Apply would fail on.
		if info.IsDir() {
			return RepairPlan{}, fmt.Errorf("%s is a directory, not a managed file", file.Path)
		}
		if fileDrifted(info, file) {
			action.Reason = RepairReasonDrifted
			plan.Actions = append(plan.Actions, action)
			continue
		}
		body, err := os.ReadFile(file.Path)
		if err != nil {
			if IsMissingOrBlocked(err) {
				action.Reason = RepairReasonMissing
				plan.Actions = append(plan.Actions, action)
				continue
			}
			return RepairPlan{}, err
		}
		if string(body) != file.Content {
			action.Reason = RepairReasonDrifted
			plan.Actions = append(plan.Actions, action)
		}
	}
	return plan, nil
}

// fileDrifted reports metadata-only drift: wrong kind (a managed path must be
// a regular file — a symlink placeholder could redirect secrets), wrong
// permission bits, or wrong owner when an owner contract is attached.
func fileDrifted(info os.FileInfo, file File) bool {
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return true
	}
	if info.Mode().Perm() != file.Mode {
		return true
	}
	if file.Owner != nil {
		if uid, gid, ok := ownerOf(info); ok && (uid != file.Owner.UID || gid != file.Owner.GID) {
			return true
		}
	}
	return false
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
		if err := writeFile(action.Path, action.Content, action.Mode, action.DirMode); err != nil {
			return RepairResult{}, err
		}
		// Ownership is part of the repair contract when attached: a repaired
		// file that stays root:root breaks the runtime readers the same way a
		// wrong mode does, so a chown failure fails the apply (audit #518).
		if action.Owner != nil {
			if err := chownFile(action.Path, action.Owner.UID, action.Owner.GID); err != nil {
				return RepairResult{}, fmt.Errorf("restore owner on %s: %w", action.Path, err)
			}
		}
		result.WrittenFiles = append(result.WrittenFiles, action.Path)
	}
	return result, nil
}

// managedParentMode is the mode applied to parent directories created for a
// managed file. 0o750 keeps freshly-created config parents from being
// world-traversable before the ownership pass runs (audit #519).
const managedParentMode = 0o750

func WriteFile(path string, content string, mode os.FileMode) error {
	return writeFile(path, content, mode, 0)
}

func writeFile(path string, content string, mode os.FileMode, dirMode os.FileMode) error {
	if dirMode == 0 {
		dirMode = managedParentMode
	}
	return atomicfile.Write(path, []byte(content), mode, dirMode)
}

func IsMissingOrBlocked(err error) bool {
	return os.IsNotExist(err) || errors.Is(err, syscall.ENOTDIR)
}
