package runtimeinstall

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mikkelchokolate/Veil/internal/atomicfile"
)

const runtimeActivationJournalName = ".activation-journal.json"

var runtimeActivationRemove = os.Remove

type runtimeActivationJournal struct {
	Version          int       `json:"version"`
	Runtime          string    `json:"runtime"`
	Binary           string    `json:"binary"`
	ActivePath       string    `json:"activePath"`
	NewTarget        string    `json:"newTarget"`
	NewDigest        string    `json:"newDigest"`
	PreviousTarget   string    `json:"previousTarget,omitempty"`
	HadPrevious      bool      `json:"hadPrevious"`
	PreviousRegular  bool      `json:"previousRegular"`
	HadManifest      bool      `json:"hadManifest"`
	PreviousManifest []byte    `json:"previousManifest,omitempty"`
	Phase            string    `json:"phase"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

func beginRuntimeActivation(storeRoot string, runtime Runtime, active, target, digest string, now time.Time) (runtimeActivationJournal, error) {
	if err := recoverRuntimeActivation(storeRoot); err != nil {
		return runtimeActivationJournal{}, err
	}
	previousTarget, hadPrevious, previousRegular, err := inspectPreviousRuntime(active, storeRoot, runtime)
	if err != nil {
		return runtimeActivationJournal{}, err
	}
	manifestPath := filepath.Join(storeRoot, "manifest.json")
	manifestBody, manifestErr := os.ReadFile(manifestPath)
	hadManifest := manifestErr == nil
	if manifestErr != nil && !errors.Is(manifestErr, os.ErrNotExist) {
		return runtimeActivationJournal{}, manifestErr
	}
	journal := runtimeActivationJournal{
		Version: 1, Runtime: runtime.Name, Binary: runtime.Binary, ActivePath: active,
		NewTarget: target, NewDigest: digest, PreviousTarget: previousTarget,
		HadPrevious: hadPrevious, PreviousRegular: previousRegular,
		HadManifest: hadManifest, PreviousManifest: manifestBody,
		Phase: "prepared", UpdatedAt: now,
	}
	if err := writeRuntimeActivationJournal(storeRoot, journal); err != nil {
		return runtimeActivationJournal{}, err
	}
	return journal, nil
}

func inspectPreviousRuntime(active, storeRoot string, runtime Runtime) (string, bool, bool, error) {
	info, err := os.Lstat(active)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, false, nil
	}
	if err != nil {
		return "", false, false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(active)
		return target, err == nil, false, err
	}
	if !info.Mode().IsRegular() {
		return "", false, false, fmt.Errorf("active runtime %s is not regular or symlink", active)
	}
	digest, err := runtimeFileSHA256(active)
	if err != nil {
		return "", false, false, err
	}
	return filepath.Join(storeRoot, runtime.Name, "legacy", digest, runtime.Binary), true, true, nil
}

func updateRuntimeActivationPhase(storeRoot string, journal *runtimeActivationJournal, phase string, now time.Time) error {
	journal.Phase, journal.UpdatedAt = phase, now
	return writeRuntimeActivationJournal(storeRoot, *journal)
}

func writeRuntimeActivationJournal(storeRoot string, journal runtimeActivationJournal) error {
	body, err := json.Marshal(journal)
	if err != nil {
		return err
	}
	return atomicfile.Write(filepath.Join(storeRoot, runtimeActivationJournalName), body, 0o600, 0o755)
}

func removeRuntimeActivationJournal(storeRoot string) error {
	path := filepath.Join(storeRoot, runtimeActivationJournalName)
	if err := runtimeActivationRemove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return syncRuntimeDirectory(storeRoot)
}

func recoverRuntimeActivation(storeRoot string) error {
	path := filepath.Join(storeRoot, runtimeActivationJournalName)
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var journal runtimeActivationJournal
	if err := json.Unmarshal(body, &journal); err != nil {
		return fmt.Errorf("decode runtime activation journal: %w", err)
	}
	if err := validateRuntimeActivationJournal(storeRoot, journal); err != nil {
		return err
	}
	if journal.Phase == "manifest-committed" {
		if err := verifyCommittedRuntimeActivation(storeRoot, journal); err != nil {
			return err
		}
		return removeRuntimeActivationJournal(storeRoot)
	}
	return rollbackRuntimeActivation(storeRoot, journal)
}

func validateRuntimeActivationJournal(storeRoot string, journal runtimeActivationJournal) error {
	if journal.Version != 1 || journal.Runtime == "" || journal.Binary == "" || journal.ActivePath == "" || journal.NewTarget == "" {
		return errors.New("invalid runtime activation journal")
	}
	binRoot := filepath.Dir(storeRoot)
	if filepath.Dir(journal.ActivePath) != binRoot || !pathInside(storeRoot, journal.NewTarget) {
		return errors.New("runtime activation journal escapes managed roots")
	}
	if journal.HadPrevious && journal.PreviousTarget == "" {
		return errors.New("runtime activation journal has no previous target")
	}
	if journal.PreviousRegular && !pathInside(storeRoot, journal.PreviousTarget) {
		return errors.New("legacy runtime target escapes immutable store")
	}
	return nil
}

func rollbackRuntimeActivation(storeRoot string, journal runtimeActivationJournal) error {
	if journal.HadPrevious {
		if journal.PreviousRegular {
			if _, err := os.Stat(journal.PreviousTarget); err == nil {
				if err := os.Remove(journal.ActivePath); err != nil && !errors.Is(err, os.ErrNotExist) {
					return err
				}
				if err := os.Rename(journal.PreviousTarget, journal.ActivePath); err != nil {
					return err
				}
			} else if err != nil {
				return err
			}
		} else if err := switchRuntimeSymlink(journal.ActivePath, journal.PreviousTarget); err != nil {
			return err
		}
	} else if err := os.Remove(journal.ActivePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := syncRuntimeDirectory(filepath.Dir(journal.ActivePath)); err != nil {
		return err
	}
	manifestPath := filepath.Join(storeRoot, "manifest.json")
	if journal.HadManifest {
		if err := atomicfile.Write(manifestPath, journal.PreviousManifest, 0o644, 0o755); err != nil {
			return err
		}
	} else if err := os.Remove(manifestPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return removeRuntimeActivationJournal(storeRoot)
}

func verifyCommittedRuntimeActivation(storeRoot string, journal runtimeActivationJournal) error {
	target, err := os.Readlink(journal.ActivePath)
	if err != nil || filepath.Clean(target) != filepath.Clean(journal.NewTarget) {
		return errors.New("committed runtime active link mismatch")
	}
	digest, err := runtimeFileSHA256(journal.NewTarget)
	if err != nil || digest != journal.NewDigest {
		return errors.New("committed runtime target digest mismatch")
	}
	body, err := os.ReadFile(filepath.Join(storeRoot, "manifest.json"))
	if err != nil {
		return err
	}
	var manifest runtimeManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return err
	}
	entry, ok := manifest.Runtimes[journal.Runtime]
	if !ok || entry.SHA256 != journal.NewDigest || filepath.Clean(entry.Path) != filepath.Clean(journal.NewTarget) {
		return errors.New("committed runtime manifest mismatch")
	}
	return nil
}

func pathInside(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != "." && relative != ".." && !filepath.IsAbs(relative) && len(relative) > 0 && relative[:1] != "."
}
