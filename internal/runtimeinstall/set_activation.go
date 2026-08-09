package runtimeinstall

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	runtimeSetJournalName  = ".veil-runtime-set-activation.json"
	runtimeSetManifestName = ".veil-runtime-generation.json"
	runtimeSetLockName     = ".veil-runtime-install.lock"
)

type runtimeSetItem struct {
	Name      string `json:"name"`
	Target    string `json:"target"`
	Staged    string `json:"staged"`
	Backup    string `json:"backup,omitempty"`
	HadOld    bool   `json:"hadOld"`
	OldDigest string `json:"oldDigest,omitempty"`
	NewDigest string `json:"newDigest"`
	Activated bool   `json:"activated"`
}

type runtimeSetJournal struct {
	Version       int              `json:"version"`
	TransactionID string           `json:"transactionId"`
	Phase         string           `json:"phase"`
	Items         []runtimeSetItem `json:"items"`
	UpdatedAt     int64            `json:"updatedAt"`
}

type runtimeGenerationManifest struct {
	Version       int               `json:"version"`
	TransactionID string            `json:"transactionId"`
	PublishedAt   int64             `json:"publishedAt"`
	Digests       map[string]string `json:"digests"`
}

func installRuntimeSet(ctx context.Context, opts Options, runtimes []Runtime) []Result {
	results := make([]Result, len(runtimes))
	if len(runtimes) == 0 {
		return results
	}
	if err := os.MkdirAll(opts.BinDir, 0o755); err != nil {
		return runtimeSetFailure(results, runtimes, fmt.Errorf("create runtime bin directory: %w", err))
	}
	lockPath := filepath.Join(opts.BinDir, runtimeSetLockName)
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE, 0o600)
	if err != nil {
		return runtimeSetFailure(results, runtimes, fmt.Errorf("open runtime activation lock: %w", err))
	}
	defer lockFile.Close()
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		return runtimeSetFailure(results, runtimes, fmt.Errorf("lock runtime activation: %w", err))
	}
	defer syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
	if err := recoverRuntimeSetActivation(opts.BinDir); err != nil {
		return runtimeSetFailure(results, runtimes, err)
	}
	if err := cleanupRuntimeStages(opts.BinDir); err != nil {
		return runtimeSetFailure(results, runtimes, err)
	}
	stageRoot, err := os.MkdirTemp(opts.BinDir, ".veil-runtime-set-stage-")
	if err != nil {
		return runtimeSetFailure(results, runtimes, err)
	}
	defer os.RemoveAll(stageRoot)
	stageOpts := opts
	stageOpts.BinDir = stageRoot
	failed := false
	for index, runtime := range runtimes {
		results[index] = installOne(ctx, stageOpts, runtime)
		if results[index].Err != nil || results[index].Skipped {
			failed = true
		}
	}
	if failed {
		err := errors.New("runtime set staging or verification failed; no live runtime was changed")
		for index := range results {
			if results[index].Err == nil {
				results[index].Err = err
			}
		}
		return results
	}
	journal, err := prepareRuntimeSetActivation(opts.BinDir, results)
	if err != nil {
		return runtimeSetFailureFromResults(results, err)
	}
	if err := activateRuntimeSet(opts.BinDir, &journal); err != nil {
		return runtimeSetFailureFromResults(results, err)
	}
	for index := range results {
		results[index].Path = filepath.Join(opts.BinDir, filepath.Base(results[index].Path))
	}
	return results
}

func runtimeSetFailure(results []Result, runtimes []Runtime, err error) []Result {
	for index := range results {
		results[index] = Result{Name: runtimes[index].Name, Binary: runtimes[index].Binary, Method: runtimes[index].Method, Err: err}
	}
	return results
}

func runtimeSetFailureFromResults(results []Result, err error) []Result {
	for index := range results {
		results[index].Err = err
	}
	return results
}

func newRuntimeTransactionID() (string, error) {
	body := make([]byte, 16)
	if _, err := rand.Read(body); err != nil {
		return "", err
	}
	return hex.EncodeToString(body), nil
}

func prepareRuntimeSetActivation(binDir string, results []Result) (runtimeSetJournal, error) {
	transactionID, err := newRuntimeTransactionID()
	if err != nil {
		return runtimeSetJournal{}, err
	}
	journal := runtimeSetJournal{Version: 1, TransactionID: transactionID, Phase: "intent", UpdatedAt: time.Now().UTC().Unix()}
	for _, result := range results {
		name := filepath.Base(result.Path)
		sourcePath, err := filepath.EvalSymlinks(result.Path)
		if err != nil {
			return runtimeSetJournal{}, err
		}
		stageRoot := filepath.Clean(filepath.Dir(result.Path))
		if sourcePath != stageRoot && !strings.HasPrefix(filepath.Clean(sourcePath), stageRoot+string(filepath.Separator)) {
			return runtimeSetJournal{}, errors.New("staged runtime escaped transaction root")
		}
		if name == "." || name == string(filepath.Separator) || name == "" {
			return runtimeSetJournal{}, errors.New("invalid staged runtime path")
		}
		target := filepath.Join(binDir, name)
		staged := filepath.Join(binDir, "."+name+".new."+transactionID)
		item := runtimeSetItem{Name: result.Name, Target: target, Staged: staged, NewDigest: result.SHA256}
		if item.NewDigest == "" {
			item.NewDigest, err = digestRuntimeFile(sourcePath)
			if err != nil {
				return runtimeSetJournal{}, err
			}
		}
		if err := copyRuntimeFile(sourcePath, staged, 0o755); err != nil {
			return runtimeSetJournal{}, err
		}
		if body, statErr := os.ReadFile(target); statErr == nil {
			item.HadOld = true
			digest := sha256.Sum256(body)
			item.OldDigest = hex.EncodeToString(digest[:])
			item.Backup = filepath.Join(binDir, "."+name+".old."+transactionID)
			if err := copyRuntimeFile(target, item.Backup, 0o755); err != nil {
				return runtimeSetJournal{}, err
			}
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return runtimeSetJournal{}, statErr
		}
		journal.Items = append(journal.Items, item)
	}
	if err := writeRuntimeJSONAtomic(filepath.Join(binDir, runtimeSetJournalName), journal, 0o600); err != nil {
		return runtimeSetJournal{}, err
	}
	return journal, nil
}

func activateRuntimeSet(binDir string, journal *runtimeSetJournal) error {
	journalPath := filepath.Join(binDir, runtimeSetJournalName)
	for index := range journal.Items {
		item := &journal.Items[index]
		if err := os.Rename(item.Staged, item.Target); err != nil {
			return rollbackRuntimeSet(binDir, journal, fmt.Errorf("activate %s: %w", item.Name, err))
		}
		if err := syncRuntimeDirectory(binDir); err != nil {
			return rollbackRuntimeSet(binDir, journal, err)
		}
		actual, err := digestRuntimeFile(item.Target)
		if err != nil || actual != item.NewDigest {
			return rollbackRuntimeSet(binDir, journal, errors.New("post-activation runtime digest mismatch"))
		}
		item.Activated = true
		journal.UpdatedAt = time.Now().UTC().Unix()
		if err := writeRuntimeJSONAtomic(journalPath, journal, 0o600); err != nil {
			return rollbackRuntimeSet(binDir, journal, err)
		}
	}
	manifest := runtimeGenerationManifest{Version: 1, TransactionID: journal.TransactionID, PublishedAt: time.Now().UTC().Unix(), Digests: make(map[string]string, len(journal.Items))}
	for _, item := range journal.Items {
		manifest.Digests[filepath.Base(item.Target)] = item.NewDigest
	}
	if err := writeRuntimeJSONAtomic(filepath.Join(binDir, runtimeSetManifestName), manifest, 0o600); err != nil {
		return rollbackRuntimeSet(binDir, journal, err)
	}
	journal.Phase = "published"
	journal.UpdatedAt = time.Now().UTC().Unix()
	if err := writeRuntimeJSONAtomic(journalPath, journal, 0o600); err != nil {
		return err
	}
	return finalizeRuntimeSet(binDir, *journal)
}

func rollbackRuntimeSet(binDir string, journal *runtimeSetJournal, cause error) error {
	var rollbackErr error
	for index := len(journal.Items) - 1; index >= 0; index-- {
		item := journal.Items[index]
		if item.HadOld {
			if digest, err := digestRuntimeFile(item.Backup); err != nil || digest != item.OldDigest {
				rollbackErr = errors.Join(rollbackErr, fmt.Errorf("runtime backup evidence invalid for %s", item.Name))
				continue
			}
			if err := copyRuntimeFile(item.Backup, item.Target, 0o755); err != nil {
				rollbackErr = errors.Join(rollbackErr, err)
			}
		} else if err := os.Remove(item.Target); err != nil && !errors.Is(err, os.ErrNotExist) {
			rollbackErr = errors.Join(rollbackErr, err)
		}
		_ = os.Remove(item.Staged)
	}
	_ = syncRuntimeDirectory(binDir)
	if rollbackErr != nil {
		return errors.Join(cause, fmt.Errorf("runtime set rollback incomplete: %w", rollbackErr))
	}
	journal.Phase = "rolled_back"
	journal.UpdatedAt = time.Now().UTC().Unix()
	if err := writeRuntimeJSONAtomic(filepath.Join(binDir, runtimeSetJournalName), journal, 0o600); err != nil {
		return errors.Join(cause, err)
	}
	if err := finalizeRuntimeSet(binDir, *journal); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

func recoverRuntimeSetActivation(binDir string) error {
	journalPath := filepath.Join(binDir, runtimeSetJournalName)
	body, err := os.ReadFile(journalPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var journal runtimeSetJournal
	if err := json.Unmarshal(body, &journal); err != nil || journal.Version != 1 || journal.TransactionID == "" || len(journal.Items) == 0 {
		return errors.New("invalid runtime set activation journal")
	}
	allNew := true
	for _, item := range journal.Items {
		digest, digestErr := digestRuntimeFile(item.Target)
		if digestErr != nil || digest != item.NewDigest {
			allNew = false
			break
		}
	}
	if allNew {
		var manifest runtimeGenerationManifest
		manifestBody, readErr := os.ReadFile(filepath.Join(binDir, runtimeSetManifestName))
		if readErr == nil && json.Unmarshal(manifestBody, &manifest) == nil && manifest.TransactionID == journal.TransactionID {
			return finalizeRuntimeSet(binDir, journal)
		}
	}
	err = rollbackRuntimeSet(binDir, &journal, errors.New("recover incomplete runtime set activation"))
	if _, statErr := os.Stat(journalPath); errors.Is(statErr, os.ErrNotExist) {
		return nil
	}
	return err
}

func finalizeRuntimeSet(binDir string, journal runtimeSetJournal) error {
	for _, item := range journal.Items {
		for _, path := range []string{item.Staged, item.Backup} {
			if path == "" {
				continue
			}
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	if err := os.Remove(filepath.Join(binDir, runtimeSetJournalName)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return syncRuntimeDirectory(binDir)
}

func copyRuntimeFile(source, target string, mode os.FileMode) (returnErr error) {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("runtime activation source is not a regular file")
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := target + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		if out != nil {
			if closeErr := out.Close(); returnErr == nil && closeErr != nil {
				returnErr = closeErr
			}
		}
		if !ok {
			os.Remove(tmp)
		}
	}()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	if err := out.Sync(); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	out = nil
	if err := os.Rename(tmp, target); err != nil {
		return err
	}
	ok = true
	return syncRuntimeDirectory(filepath.Dir(target))
}

func digestRuntimeFile(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:]), nil
}

func writeRuntimeJSONAtomic(path string, value any, mode os.FileMode) (returnErr error) {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		if file != nil {
			if closeErr := file.Close(); returnErr == nil && closeErr != nil {
				returnErr = closeErr
			}
		}
		if !ok {
			os.Remove(tmp)
		}
	}()
	if _, err := file.Write(body); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	file = nil
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	ok = true
	return syncRuntimeDirectory(filepath.Dir(path))
}
