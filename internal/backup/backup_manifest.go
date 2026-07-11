package backup

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/mikkelchokolate/Veil/internal/atomicfile"
)

// manifestMarshal is overridable in tests to inject marshal failures.
var manifestMarshal = json.Marshal

type ManifestStore struct {
	Path string
}

type BackupManifestStore = ManifestStore

func NewManifestStore(path string) ManifestStore {
	return ManifestStore{Path: path}
}

func NewBackupManifestStore(path string) BackupManifestStore {
	return NewManifestStore(path)
}

func (s ManifestStore) Save(manifest Manifest) error {
	manifestData, err := manifestMarshal(manifest)
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	if err := atomicfile.Write(s.Path, manifestData, 0o600, 0o700); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	return nil
}

func (s ManifestStore) Load() (Manifest, error) {
	manifestData, err := os.ReadFile(s.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return Manifest{}, fmt.Errorf("manifest not found: %w", os.ErrNotExist)
		}
		return Manifest{}, fmt.Errorf("read manifest: %w", err)
	}
	var manifest Manifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("unmarshal manifest: %w", err)
	}
	return manifest, nil
}
