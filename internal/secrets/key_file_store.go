package secrets

import (
	"crypto/rand"
	"fmt"
	"io/fs"
	"os"

	"errors"
)

type KeyFileStore struct {
	Path string
}

func NewKeyFileStore(path string) KeyFileStore { return KeyFileStore{Path: path} }

// LoadOrCreate reads a 32-byte key from path. If the file does not exist,
// a new random key is generated and written with mode 0600. If the file exists
// but has wrong permissions, they are fixed to 0600. If the file exists but
// is not exactly 32 bytes, an error is returned.
func (s KeyFileStore) LoadOrCreate() (*[KeySize]byte, error) {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("secrets: read key file %s: %w", s.Path, err)
		}
		return s.create()
	}
	if info, err := os.Stat(s.Path); err == nil {
		if info.Mode().Perm() != 0o600 {
			if err := os.Chmod(s.Path, 0o600); err != nil {
				return nil, fmt.Errorf("secrets: chmod key file %s: %w", s.Path, err)
			}
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
	if _, err := rand.Read(key[:]); err != nil {
		return nil, fmt.Errorf("secrets: generate key: %w", err)
	}
	if err := os.WriteFile(s.Path, key[:], 0o600); err != nil {
		return nil, fmt.Errorf("secrets: write key file %s: %w", s.Path, err)
	}
	return &key, nil
}
