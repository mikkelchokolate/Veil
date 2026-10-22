package managedfiles

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/mikkelchokolate/Veil/internal/atomicfile"
	"github.com/mikkelchokolate/Veil/internal/safefs"
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
	files, err := uniqueFiles(s.files)
	if err != nil {
		return RepairPlan{}, err
	}
	plan := RepairPlan{}
	for _, file := range files {
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
		reason, err := compareContent(file, info)
		if err != nil {
			return RepairPlan{}, err
		}
		if reason != "" {
			action.Reason = reason
			plan.Actions = append(plan.Actions, action)
		}
	}
	return plan, nil
}

// compareContent reads the managed file through an O_NOFOLLOW|O_NONBLOCK
// descriptor and judges the inode that descriptor pins, not the path (#1292
// F2). The earlier Lstat only classifies the leaf; between it and the read
// the leaf can be swapped. A path-based os.ReadFile would then follow a
// planted symlink (comparing attacker-chosen content) or block forever on a
// FIFO, wedging `veil repair`. Here a swapped-in symlink fails the open
// (ELOOP), a FIFO opens without blocking and is caught by fstat, and a
// regular file swapped for a different inode fails os.SameFile. Every swap
// is reported as drift so Apply atomically replaces the leaf. The read is
// bounded to one byte past the desired content: a longer file is drift
// regardless of what follows, and a huge planted file is never slurped.
func compareContent(file File, lstatInfo os.FileInfo) (RepairReason, error) {
	if planBeforeReadHook != nil {
		planBeforeReadHook(file.Path)
	}
	handle, err := safefs.OpenNoFollow(file.Path)
	if err != nil {
		if IsMissingOrBlocked(err) {
			return RepairReasonMissing, nil
		}
		// The leaf changed kind after Lstat (symlink → ELOOP, socket →
		// ENXIO, or the non-Linux lstat-based refusal). Re-classify it
		// rather than failing the whole plan on a swapped leaf.
		current, lerr := os.Lstat(file.Path)
		if lerr != nil {
			if IsMissingOrBlocked(lerr) {
				return RepairReasonMissing, nil
			}
			return "", errors.Join(err, lerr)
		}
		if !current.Mode().IsRegular() || !os.SameFile(lstatInfo, current) {
			return RepairReasonDrifted, nil
		}
		return "", err
	}
	defer handle.Close()
	info, err := handle.Stat()
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory, not a managed file", file.Path)
	}
	if !os.SameFile(lstatInfo, info) || fileDrifted(info, file) {
		return RepairReasonDrifted, nil
	}
	body, err := io.ReadAll(io.LimitReader(handle, int64(len(file.Content))+1))
	if err != nil {
		return "", err
	}
	if string(body) != file.Content {
		return RepairReasonDrifted, nil
	}
	return "", nil
}

// planBeforeReadHook lets tests swap the leaf between Plan's Lstat and its
// descriptor open. Always nil in production.
var planBeforeReadHook func(path string)

// uniqueFiles collapses entries that name the same path. Identical
// duplicates are harmless and kept once; duplicates that disagree on
// content, mode, directory mode or owner would make every repair rewrite the
// file with whichever entry ran last and report it drifted again on the next
// plan, so they fail the plan instead (#1292 F2).
func uniqueFiles(files []File) ([]File, error) {
	seen := make(map[string]File, len(files))
	out := make([]File, 0, len(files))
	for _, file := range files {
		key := filepath.Clean(file.Path)
		if prev, ok := seen[key]; ok {
			if !sameFileSpec(prev, file) {
				return nil, fmt.Errorf("managed file %s is listed twice with conflicting content, mode or owner", file.Path)
			}
			continue
		}
		seen[key] = file
		out = append(out, file)
	}
	return out, nil
}

func sameFileSpec(a, b File) bool {
	if a.Content != b.Content || a.Mode != b.Mode || a.DirMode != b.DirMode {
		return false
	}
	if (a.Owner == nil) != (b.Owner == nil) {
		return false
	}
	return a.Owner == nil || *a.Owner == *b.Owner
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
