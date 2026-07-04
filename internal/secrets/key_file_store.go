package secrets

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

type KeyFileStore struct {
	Path string
}

func NewKeyFileStore(path string) KeyFileStore { return KeyFileStore{Path: path} }

// LoadOrCreate reads a 32-byte key from path. If the file does not exist,
// a new random key is generated and written with mode 0600. Modes 0600
// (owner-only) and 0640 (owner plus group-read) are both accepted as secure:
// 0640 lets a group-scoped service account read a root-owned key. Any other
// mode is tightened to 0600 on a best-effort basis — the key bytes are already
// read, so an unappliable chmod (e.g. a read-only /etc/veil mount or a key the
// process does not own) must not fail the load. If the file exists but is not
// exactly 32 bytes, an error is returned.
func (s KeyFileStore) LoadOrCreate() (*[KeySize]byte, error) {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("secrets: read key file %s: %w", s.Path, err)
		}
		return s.create()
	}
	if info, err := os.Stat(s.Path); err == nil {
		if perm := info.Mode().Perm(); perm != 0o600 && perm != 0o640 {
			// Best-effort hardening; do not fail the load when it cannot be applied.
			_ = os.Chmod(s.Path, 0o600)
		}
	}
	if len(data) != KeySize {
		return nil, fmt.Errorf("secrets: key file %s has wrong length: %d bytes (expected %d)", s.Path, len(data), KeySize)
	}
	var key [KeySize]byte
	copy(key[:], data)
	return &key, nil
}

func (s KeyFileStore) create() (*[KeySize]byte, error) {
	var key [KeySize]byte
	if _, err := randRead(key[:]); err != nil {
		return nil, fmt.Errorf("secrets: generate key: %w", err)
	}
	if err := os.WriteFile(s.Path, key[:], 0o600); err != nil {
		return nil, fmt.Errorf("secrets: write key file %s: %w", s.Path, err)
	}
	return &key, nil
}
