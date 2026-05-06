package api

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/veil-panel/veil/internal/secrets"
)

type StateStore struct {
	path   string
	cipher *secrets.Cipher
}

func NewStateStore(path string, cipher *secrets.Cipher) StateStore {
	return StateStore{path: path, cipher: cipher}
}

func (s StateStore) Load() (managementSnapshot, bool, error) {
	if s.path == "" {
		return managementSnapshot{}, false, nil
	}
	body, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return managementSnapshot{}, false, nil
		}
		return managementSnapshot{}, false, err
	}
	var snapshot managementSnapshot
	if err := json.Unmarshal(body, &snapshot); err != nil {
		return managementSnapshot{}, false, err
	}
	s.decryptSnapshot(&snapshot)
	return snapshot, true, nil
}

func (s StateStore) Save(snapshot managementSnapshot) error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	body, err := s.Marshal(snapshot)
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s StateStore) Marshal(snapshot managementSnapshot) ([]byte, error) {
	s.encryptSnapshot(&snapshot)
	body, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(body, '\n'), nil
}

func (s StateStore) encryptSnapshot(snapshot *managementSnapshot) {
	if s.cipher == nil {
		return
	}
	encrypt := func(v string) string {
		if v == "" || secrets.IsEncrypted(v) {
			return v
		}
		if enc, err := s.cipher.Encrypt(v); err == nil {
			return enc
		}
		return v
	}
	stateSecretPolicy{}.Transform(snapshot, encrypt)
}

func (s StateStore) decryptSnapshot(snapshot *managementSnapshot) {
	if s.cipher == nil {
		return
	}
	decrypt := func(v string) string {
		if v == "" {
			return v
		}
		if dec, err := s.cipher.Decrypt(v); err == nil {
			return dec
		}
		return v
	}
	stateSecretPolicy{}.Transform(snapshot, decrypt)
}
