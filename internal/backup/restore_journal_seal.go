package backup

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mikkelchokolate/Veil/internal/safefs"
)

// restoreJournalSealName is the per-transaction journal authentication key.
// It lives next to state.key — a directory the restorer must be able to
// write anyway to publish the key, and which the service uid cannot write
// (/etc/veil is root:veil 0751) — never in the service-writable state root
// that hosts the journal itself. The name deliberately avoids the
// ".veil-restore-" residue prefix so the crash sweeper never collects it and
// a journal can never reference it as a staged leaf (#1256).
const restoreJournalSealName = ".veil-journal-seal.key"

const restoreJournalSealKeySize = 32

// restoreJournalSealDomain separates the journal MAC from any other use of
// HMAC-SHA256 in the tree.
const restoreJournalSealDomain = "veil-restore-journal-v2\x00"

// The journal lives in the service-writable state root, so every field in it
// is attacker-authored, and every digest it records is computable by anyone
// who can merely READ the live target (state.key is root:veil 0640). Digest
// equality therefore cannot prove "this transaction published it" (#1256).
// The writer instead MACs every journal revision with a fresh random key it
// keeps in the key directory (owner-only, unreadable by the service uid);
// root-side recovery refuses any journal that does not verify. The key is
// rotated at every prepare and retired once the journal is gone, so a
// genuine journal captured mid-flight cannot be replayed after its
// transaction finished.

// newRestoreJournalSealKey creates a fresh seal key in keyDirPath, replacing
// any stale one. The create is O_EXCL|O_NOFOLLOW relative to the pinned
// directory, so the key is always a new inode owned by the restorer.
func newRestoreJournalSealKey(keyDirPath string) ([]byte, error) {
	dir, err := safefs.OpenDir(keyDirPath)
	if err != nil {
		return nil, fmt.Errorf("open restore journal seal directory: %w", err)
	}
	defer dir.Close()
	if err := dir.RemoveAt(restoreJournalSealName); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("retire stale restore journal seal: %w", err)
	}
	key := make([]byte, restoreJournalSealKeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	file, err := dir.CreateFileAt(restoreJournalSealName, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create restore journal seal: %w", err)
	}
	if _, err := file.Write(key); err != nil {
		_ = file.Close()
		_ = dir.RemoveAt(restoreJournalSealName)
		return nil, err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = dir.RemoveAt(restoreJournalSealName)
		return nil, err
	}
	if err := file.Close(); err != nil {
		_ = dir.RemoveAt(restoreJournalSealName)
		return nil, err
	}
	if err := dir.File().Sync(); err != nil {
		return nil, err
	}
	return key, nil
}

// loadRestoreJournalSealKey reads the live seal key. It must be a
// single-linked regular file owned by this process and inaccessible to
// group/other — anything else could have been read or planted by the service
// uid and proves nothing.
func loadRestoreJournalSealKey(keyDirPath string) ([]byte, error) {
	dir, err := safefs.OpenDir(keyDirPath)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	file, err := dir.OpenFileAt(restoreJournalSealName)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || unsafeJournalHardLink(info) || untrustedRestoreJournalSeal(info) {
		return nil, errors.New("restore journal seal key is not a private owner-only file")
	}
	key, err := io.ReadAll(io.LimitReader(file, restoreJournalSealKeySize+1))
	if err != nil {
		return nil, err
	}
	if len(key) != restoreJournalSealKeySize {
		return nil, errors.New("restore journal seal key has an unexpected size")
	}
	return key, nil
}

// retireRestoreJournalSealKey removes the seal key once its journal can no
// longer be legitimately replayed. Best effort: the next prepare rotates
// the key regardless.
func retireRestoreJournalSealKey(keyDirPath string) {
	if keyDirPath == "" {
		return
	}
	dir, err := safefs.OpenDir(keyDirPath)
	if err != nil {
		return
	}
	defer dir.Close()
	if err := dir.RemoveAt(restoreJournalSealName); err == nil {
		_ = dir.File().Sync()
	}
}

// restoreJournalSealDir is the directory holding the seal key for a journal:
// the state.key member's directory.
func restoreJournalSealDir(files []restoreJournalFile) string {
	target := restoreJournalMemberTarget(files, "state.key")
	if target == "" {
		return ""
	}
	return filepath.Dir(target)
}

// computeRestoreJournalSeal MACs the canonical encoding of the journal with
// the seal field cleared.
func computeRestoreJournalSeal(key []byte, disk restoreJournalDisk) (string, error) {
	disk.Seal = ""
	body, err := json.Marshal(disk)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(restoreJournalSealDomain))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// verifyRestoreJournalSeal authenticates a decoded on-disk journal against
// the live seal key.
func verifyRestoreJournalSeal(key []byte, disk restoreJournalDisk) error {
	if !isSHA256HexDigest(disk.Seal) {
		return errors.New("restore journal is not sealed")
	}
	want, err := computeRestoreJournalSeal(key, disk)
	if err != nil {
		return err
	}
	if !hmac.Equal([]byte(want), []byte(disk.Seal)) {
		return errors.New("restore journal seal does not verify")
	}
	return nil
}
