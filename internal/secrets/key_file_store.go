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

var syncKeyFile = func(file *os.File) error { return file.Sync() }

type KeyFileStore struct {
	Path string
}

func NewKeyFileStore(path string) KeyFileStore { return KeyFileStore{Path: path} }

// LoadOrCreate reads a 32-byte key from path. If the file does not exist,
// a new random key is written to a private temporary file, synced, and then
// published without replacement. Concurrent creators therefore all return the
// single key that won the exclusive publish. Modes 0600 (owner-only) and 0640
// (owner plus group-read) are accepted as secure; any other mode is tightened
// to 0600 on a best-effort basis.
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
	data, err := os.ReadFile(s.Path)
	if err != nil {
		return nil, fmt.Errorf("secrets: read key file %s: %w", s.Path, err)
	}
	if info, err := os.Stat(s.Path); err == nil {
		if perm := info.Mode().Perm(); perm != 0o600 && perm != 0o640 {
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
