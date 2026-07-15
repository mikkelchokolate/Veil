package secrets

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

var (
	syncKeyFile  = func(file *os.File) error { return file.Sync() }
	chmodKeyFile = os.Chmod
)

type KeyFileStore struct {
	Path string
}

func NewKeyFileStore(path string) KeyFileStore { return KeyFileStore{Path: path} }

// LoadOrCreate reads a 32-byte key from path. If the file does not exist,
// a new random key is written to a private temporary file, synced, and then
// published without replacement. Concurrent creators therefore all return the
// single key that won the exclusive publish. Owner read is required; owner
// write and group read are optional. Group write/execute and all other-user
// permissions must be tightened to 0600 before the key is accepted.
func (s KeyFileStore) LoadOrCreate() (*[KeySize]byte, error) {
	key, err := s.load()
	if err == nil {
		return key, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return s.create()
}

func (s KeyFileStore) load() (*[KeySize]byte, error) {
	info, err := os.Stat(s.Path)
	if err != nil {
		return nil, fmt.Errorf("secrets: stat key file %s: %w", s.Path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("secrets: key file %s is not a regular file", s.Path)
	}
	if err := enforceKeyFilePermissions(s.Path, info.Mode().Perm()); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(s.Path)
	if err != nil {
		return nil, fmt.Errorf("secrets: read key file %s: %w", s.Path, err)
	}
	if len(data) != KeySize {
		return nil, fmt.Errorf("secrets: key file %s has wrong length: %d bytes (expected %d)", s.Path, len(data), KeySize)
	}
	var key [KeySize]byte
	copy(key[:], data)
	return &key, nil
}

func enforceKeyFilePermissions(path string, mode os.FileMode) error {
	if runtime.GOOS == "windows" || secureKeyFilePermissions(mode) {
		return nil
	}
	if err := chmodKeyFile(path, 0o600); err != nil {
		return fmt.Errorf("secrets: tighten permissions on key file %s from %04o: %w", path, mode, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("secrets: verify permissions on key file %s: %w", path, err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		return fmt.Errorf("secrets: key file %s remains insecure after chmod: %04o", path, got)
	}
	return nil
}

func secureKeyFilePermissions(mode os.FileMode) bool {
	// Allow 0400, 0440, 0600, and 0640. These all require owner read,
	// optionally allow owner write and group read, and expose nothing else.
	return mode&0o400 != 0 && mode&^os.FileMode(0o640) == 0
}

func (s KeyFileStore) create() (*[KeySize]byte, error) {
	var key [KeySize]byte
	if _, err := randRead(key[:]); err != nil {
		return nil, fmt.Errorf("secrets: generate key: %w", err)
	}
	dir := filepath.Dir(s.Path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("secrets: create key directory %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(s.Path)+".tmp-*")
	if err != nil {
		return nil, fmt.Errorf("secrets: create temporary key file in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}()

	written, err := tmp.Write(key[:])
	if err != nil {
		return nil, fmt.Errorf("secrets: write temporary key file %s: %w", tmpPath, err)
	}
	if written != KeySize {
		return nil, fmt.Errorf("secrets: write temporary key file %s: %w", tmpPath, io.ErrShortWrite)
	}
	if err := syncKeyFile(tmp); err != nil {
		return nil, fmt.Errorf("secrets: sync temporary key file %s: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("secrets: close temporary key file %s: %w", tmpPath, err)
	}

	if err := os.Link(tmpPath, s.Path); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return s.load()
		}
		return nil, fmt.Errorf("secrets: publish key file %s: %w", s.Path, err)
	}
	bestEffortSyncKeyDirectory(dir)
	return &key, nil
}

func bestEffortSyncKeyDirectory(path string) {
	if runtime.GOOS == "windows" {
		return
	}
	dir, err := os.Open(path)
	if err != nil {
		return
	}
	defer dir.Close()
	_ = dir.Sync()
}
