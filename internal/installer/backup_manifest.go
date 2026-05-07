package installer

import (
	"encoding/json"
	"fmt"
	"os"
)

type BackupManifestStore struct {
	Path string
}

func NewBackupManifestStore(path string) BackupManifestStore {
	return BackupManifestStore{Path: path}
}

func (s BackupManifestStore) Save(manifest backupManifest) error {
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	if err := os.WriteFile(s.Path, manifestData, 0o600); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	return nil
}

func (s BackupManifestStore) Load() (backupManifest, error) {
	manifestData, err := os.ReadFile(s.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return backupManifest{}, fmt.Errorf("manifest not found: %w", os.ErrNotExist)
		}
		return backupManifest{}, fmt.Errorf("read manifest: %w", err)
	}
	var manifest backupManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return backupManifest{}, fmt.Errorf("unmarshal manifest: %w", err)
	}
	return manifest, nil
}
