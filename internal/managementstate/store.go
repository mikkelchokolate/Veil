package managementstate

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/secrets"
)

type Store struct {
	path   string
	cipher *secrets.Cipher
}

type StateStore = Store

func NewStore(path string, cipher *secrets.Cipher) Store {
	return Store{path: path, cipher: cipher}
}

func NewStateStore(path string, cipher *secrets.Cipher) StateStore {
	return NewStore(path, cipher)
}

func (s Store) Load() (model.ManagementSnapshot, bool, error) {
	if s.path == "" {
		return model.ManagementSnapshot{}, false, nil
	}
	body, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return model.ManagementSnapshot{}, false, nil
		}
		return model.ManagementSnapshot{}, false, err
	}
	snapshot, err := NewManagementStateCodec().Decode(body)
	if err != nil {
		return model.ManagementSnapshot{}, false, err
	}
	if err := s.decryptSnapshot(&snapshot); err != nil {
		return model.ManagementSnapshot{}, false, err
	}
	return snapshot, true, nil
}

func (s Store) Save(snapshot model.ManagementSnapshot) error {
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

func (s Store) Marshal(snapshot model.ManagementSnapshot) ([]byte, error) {
	if err := s.encryptSnapshot(&snapshot); err != nil {
		return nil, err
	}
	return NewManagementStateCodec().Encode(snapshot)
}

func (s Store) encryptSnapshot(snapshot *model.ManagementSnapshot) error {
	if s.cipher == nil {
		return nil
	}
	encrypt := func(v string) (string, error) {
		if v == "" || secrets.IsEncrypted(v) {
			return v, nil
		}
		return s.cipher.Encrypt(v)
	}
	return SecretPolicy{}.Transform(snapshot, encrypt)
}

func (s Store) decryptSnapshot(snapshot *model.ManagementSnapshot) error {
	if s.cipher == nil {
		return nil
	}
	decrypt := func(v string) (string, error) {
		if v == "" {
			return v, nil
		}
		if !secrets.IsEncrypted(v) {
			return v, nil
		}
		return s.cipher.Decrypt(v)
	}
	return SecretPolicy{}.Transform(snapshot, decrypt)
}

func EncryptSnapshot(snapshot *model.ManagementSnapshot, cipher *secrets.Cipher) error {
	return NewStore("", cipher).encryptSnapshot(snapshot)
}

func DecryptSnapshot(snapshot *model.ManagementSnapshot, cipher *secrets.Cipher) error {
	return NewStore("", cipher).decryptSnapshot(snapshot)
}
